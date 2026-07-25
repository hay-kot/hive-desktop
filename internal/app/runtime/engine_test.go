package runtime_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// The engine is tested against a real SQLite store, like everything else that
// touches the commit protocol. Its whole job is what happens between a read
// and a commit, and a fake store would only assert that the calls were made.

func openTestStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// flowSet is a mutable stand-in for the flow store, so a test can change what
// the engine sees between reloads.
type flowSet struct {
	mu    sync.Mutex
	flows []flow.Flow
}

func (f *flowSet) List() []flow.Flow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]flow.Flow(nil), f.flows...)
}

func (f *flowSet) set(flows ...flow.Flow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flows = flows
}

// triageFlow is a github-source into one feed, the smallest flow that commits
// membership.
func triageFlow(id string, enabled bool) flow.Flow {
	return flow.Flow{
		ID:      id,
		Name:    id,
		Enabled: enabled,
		Nodes: []flow.Node{
			{ID: "src", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open"}},
			{ID: "inbox", Type: "feed", Config: &flow.FeedConfig{}},
		},
		Wires: []flow.Wire{{From: "src", To: "inbox"}},
	}
}

type observation struct {
	externalID string
	title      string
}

// ingest writes an item through the production source boundary and returns the
// resulting log offset, exactly as the producer does.
func ingest(t *testing.T, db *store.DB, flowID string, obs ...observation) int64 {
	t.Helper()
	var last int64
	for _, o := range obs {
		payload, err := json.Marshal(map[string]string{"title": o.title, "repo": "acme/app"})
		require.NoError(t, err)
		result, err := db.IngestObservation(t.Context(), passthroughClassifier{}, store.IngestObservationParams{
			ProfileID: flowID,
			Topic:     "source:" + flowID + "/src",
			Current: store.Observation{
				ExternalID: o.externalID, Title: o.title, URL: "https://example.invalid/" + o.externalID,
				SourceKind: "github", SourceScope: "search",
				ObservedAt: time.Now().UnixMilli(), Payload: payload,
			},
		})
		require.NoError(t, err)
		if result.Wrote {
			last = result.Offset
		}
	}
	return last
}

// snapshotSource appends the authoritative "this is everything the source
// currently has" event a producer tick ends with. Replay recomputes membership
// from it, so a flow with no snapshot in its log has nothing to replay.
func snapshotSource(t *testing.T, db *store.DB, flowID string, obs ...observation) {
	t.Helper()
	items := make([]store.SnapshotItem, 0, len(obs))
	for _, o := range obs {
		payload, err := json.Marshal(map[string]string{"title": o.title, "repo": "acme/app"})
		require.NoError(t, err)
		items = append(items, store.SnapshotItem{Key: o.externalID, Payload: payload})
	}
	_, err := db.AppendSnapshot(t.Context(), "source:"+flowID+"/src", "github", "search", items)
	require.NoError(t, err)
}

type passthroughClassifier struct{}

func (passthroughClassifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	kind := "observed"
	if previous != nil {
		kind = "updated"
	}
	return store.Classification{
		Kind: kind, Transition: store.TransitionNone, Attention: store.AttentionActivity,
		Lifecycle: store.LifecycleActive, Summary: current.Title,
	}
}

// committed is a latch a test waits on instead of sleeping, so a slow machine
// makes the test slower rather than flaky.
type committed struct {
	ch chan struct{}
}

func newCommitted() *committed { return &committed{ch: make(chan struct{}, 64)} }

func (c *committed) signal() {
	select {
	case c.ch <- struct{}{}:
	default:
	}
}

func (c *committed) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the engine did not report a commit within 5s")
	}
}

func startEngine(t *testing.T, db *store.DB, flows *flowSet, onCommit func()) *runtime.Engine {
	t.Helper()
	engine := runtime.NewEngine(runtime.EngineOptions{
		Store:       db,
		Flows:       flows,
		Scripts:     testScripts(),
		Logger:      zerolog.Nop(),
		OnCommitted: onCommit,
	})
	require.NoError(t, engine.Start(t.Context()))
	t.Cleanup(engine.Stop)
	return engine
}

// Start installs flows synchronously, so a wake-up that arrives immediately
// afterwards always finds a runner. That is what removed the readiness latch
// the browser runtime needed a test hook for.
func TestEngineRoutesAnAppendIntoAFeed(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	flows := &flowSet{}
	flows.set(triageFlow("triage", true))

	done := newCommitted()
	engine := startEngine(t, db, flows, done.signal)

	ingest(t, db, "triage", observation{"pr-1", "First"}, observation{"pr-2", "Second"})
	engine.Wake()
	done.wait(t)

	items, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 10)
	require.NoError(t, err)
	require.Len(t, items, 2)

	runs, err := db.NodeRuns(t.Context(), "triage", 10)
	require.NoError(t, err)
	require.NotEmpty(t, runs)
}

// A wake-up that lands while a pass is already running must not be lost. It is
// a latch rather than a queue, so the pass in flight is followed by exactly
// one more — enough to see the append, without piling up.
func TestEngineWakeIsLevelTriggered(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	flows := &flowSet{}
	flows.set(triageFlow("triage", true))

	done := newCommitted()
	engine := startEngine(t, db, flows, done.signal)

	for i := range 25 {
		ingest(t, db, "triage", observation{externalID: "pr-" + string(rune('a'+i)), title: "Item"})
		engine.Wake()
	}

	require.Eventually(t, func() bool {
		items, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 100)
		return err == nil && len(items) == 25
	}, 10*time.Second, 20*time.Millisecond, "every append must eventually be routed")
}

// A disabled flow is not merely skipped: its runner is dropped, so nothing it
// used to hold survives a toggle.
func TestEngineDropsARunnerWhenItsFlowIsDisabled(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	flows := &flowSet{}
	flows.set(triageFlow("triage", true))

	done := newCommitted()
	engine := startEngine(t, db, flows, done.signal)

	ingest(t, db, "triage", observation{"pr-1", "First"})
	engine.Wake()
	done.wait(t)

	flows.set(triageFlow("triage", false))
	engine.Reload()

	ingest(t, db, "triage", observation{"pr-2", "Second"})
	engine.Wake()

	require.Never(t, func() bool {
		items, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 10)
		return err == nil && len(items) > 1
	}, time.Second, 50*time.Millisecond, "a disabled flow must stop routing")
}

// The replay protocol is what makes a flow edit change what a feed contains
// without re-running the actions already run for those items. Removing the
// feed node must therefore take its claims with it.
func TestEngineReplayReconcilesMembershipOnReload(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	flows := &flowSet{}
	flows.set(triageFlow("triage", true))

	done := newCommitted()
	engine := startEngine(t, db, flows, done.signal)

	ingest(t, db, "triage", observation{"pr-1", "First"})
	snapshotSource(t, db, "triage", observation{"pr-1", "First"})
	engine.Wake()
	done.wait(t)

	require.Eventually(t, func() bool {
		items, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 10)
		return err == nil && len(items) == 1
	}, 5*time.Second, 20*time.Millisecond)

	// Same source, different feed node id. The old feed's claims belong to
	// structure that no longer exists.
	replacement := triageFlow("triage", true)
	replacement.Nodes[1].ID = "other"
	replacement.Wires = []flow.Wire{{From: "src", To: "other"}}
	flows.set(replacement)
	engine.Reload()

	require.Eventually(t, func() bool {
		stale, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 10)
		if err != nil || len(stale) != 0 {
			return false
		}
		moved, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/other", 10)
		return err == nil && len(moved) == 1
	}, 5*time.Second, 20*time.Millisecond, "replay recomputes membership from the source's latest snapshot")
}

// A flow that cannot be built keeps its predecessor in service. Silently
// taking a working flow offline for an authoring mistake would lose messages
// the last-known-good version handles fine.
func TestEngineKeepsTheLastGoodRunnerWhenAReloadFails(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	flows := &flowSet{}
	flows.set(triageFlow("triage", true))

	done := newCommitted()
	var failures []string
	engine := runtime.NewEngine(runtime.EngineOptions{
		Store:       db,
		Flows:       flows,
		Scripts:     testScripts(),
		Logger:      zerolog.Nop(),
		OnCommitted: done.signal,
		OnFlowError: func(flowID string, _ error) { failures = append(failures, flowID) },
	})
	require.NoError(t, engine.Start(t.Context()))
	t.Cleanup(engine.Stop)

	ingest(t, db, "triage", observation{"pr-1", "First"})
	engine.Wake()
	done.wait(t)

	broken := triageFlow("triage", true)
	broken.Wires = append(broken.Wires, flow.Wire{From: "inbox", To: "src"}) // a cycle
	flows.set(broken)
	engine.Reload()

	ingest(t, db, "triage", observation{"pr-2", "Second"})
	engine.Wake()

	require.Eventually(t, func() bool {
		items, err := db.ListInboxItemsByFeed(t.Context(), "triage", "triage/inbox", 10)
		return err == nil && len(items) == 2
	}, 5*time.Second, 20*time.Millisecond, "the previous runner must keep routing")
	require.Contains(t, failures, "triage", "and the failure must be reported, or the app silently runs an older graph")
}

// Stop is the single teardown path and has to survive being called on an
// engine that was never started — App.Close runs whether Start did or not.
func TestEngineStopWithoutStart(t *testing.T) {
	t.Parallel()

	engine := runtime.NewEngine(runtime.EngineOptions{
		Store: openTestStore(t), Flows: &flowSet{}, Scripts: testScripts(), Logger: zerolog.Nop(),
	})
	require.NotPanics(t, engine.Stop)
	require.NotPanics(t, engine.Stop)
}
