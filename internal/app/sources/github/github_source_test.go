package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
)

// openTestPipelineDB opens a throwaway store on a temp dir, migrated and
// closed with the test.
func openTestPipelineDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// fakeFlows is an in-memory FlowLister for the source-lister tests.
type fakeFlows []flow.Flow

func (f fakeFlows) List() []flow.Flow { return f }

// singleSearchAPI serves one search item and counts requests, so tests can
// prove githubSource routes through LiveProvider's cache/singleflight
// instead of fetching on every call.
type singleSearchAPI struct {
	calls   atomic.Int32
	aliases atomic.Int32
}

func (a *singleSearchAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		a.calls.Add(1)
		var request struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		aliases := strings.Count(request.Query, ": search(")
		a.aliases.Store(int32(aliases))
		w.Header().Set("Content-Type", "application/json")
		item := map[string]any{
			"__typename": "PullRequest",
			"number":     7,
			"title":      "fix the thing",
			"state":      "OPEN",
			"url":        "https://github.com/o/r/pull/7",
			"isDraft":    false,
			"author":     map[string]any{"login": "hayden"},
			"repository": map[string]any{"nameWithOwner": "o/r"},
			"labels":     map[string]any{"nodes": []any{}},
			"updatedAt":  "2026-07-18T10:00:00Z",
			"createdAt":  "2026-07-17T00:00:00Z",
		}
		data := make(map[string]any, aliases)
		for i := range aliases {
			data[fmt.Sprintf("s%d", i)] = map[string]any{"nodes": []any{item}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("/notifications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	return mux
}

// newLiveProviderFixture constructs a real feed.LiveProvider against a fake
// GitHub API. Source config now lives in the flow's github-source nodes, not
// a profiles config, so the provider needs no store.
func newLiveProviderFixture(t *testing.T, api *singleSearchAPI) *feed.LiveProvider {
	t.Helper()
	server := httptest.NewServer(api.handler())
	t.Cleanup(server.Close)

	client := github.NewClient(github.WithAPIBase(server.URL))
	return feed.NewLiveProvider(client, github.NewMemoryTokenStore("tok"), zerolog.Nop())
}

func TestGithubSource_Produce_EmitsWireItems(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)

	src := &githubSource{live: live, def: feed.SourceDef{ID: "triage/in-prs", Kind: "search", Query: "is:open is:pr author:@me"}, topic: "source:triage/in-prs"}

	var emitted []ingest.Msg
	err := src.Produce(context.Background(), func(msg ingest.Msg) error {
		emitted = append(emitted, msg)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, emitted, 1)

	msg := emitted[0]
	assert.Equal(t, "source:triage/in-prs", msg.Topic)
	assert.Equal(t, "o/r#7", msg.Key)

	var item feed.Item
	require.NoError(t, json.Unmarshal(msg.Payload, &item))
	assert.Equal(t, "o/r#7", item.ID)
	assert.Equal(t, "fix the thing", item.Title)
}

// TestGithubSource_Produce_ReusesCoalescedFetch is the point of the seam:
// githubSource must not implement its own fetching. It should route through
// LiveProvider.SourceItems (cache + singleflight + conditional requests),
// so repeated Produce calls within the cache TTL cost no extra API request.
func TestGithubSource_Produce_ReusesCoalescedFetch(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)
	src := &githubSource{live: live, def: feed.SourceDef{ID: "triage/in-prs", Kind: "search", Query: "is:open is:pr author:@me"}, topic: "source:triage/in-prs"}

	for range 3 {
		err := src.Produce(context.Background(), func(ingest.Msg) error { return nil })
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), api.calls.Load(), "three Produce calls within the cache TTL should cost one API request")
}

func TestGithubSource_Produce_PropagatesFetchError(t *testing.T) {
	t.Parallel()

	// No token: LiveProvider.SourceItems fails with ErrNotAuthenticated
	// before ever hitting the network.
	live := feed.NewLiveProvider(github.NewClient(), github.NewMemoryTokenStore(""), zerolog.Nop())
	src := &githubSource{live: live, def: feed.SourceDef{ID: "triage/in-prs", Kind: "search", Query: "is:open"}, topic: "source:triage/in-prs"}

	called := false
	err := src.Produce(context.Background(), func(ingest.Msg) error {
		called = true
		return nil
	})
	require.Error(t, err)
	assert.False(t, called, "no items should be emitted when the fetch fails")
}

func TestNewFlowSourceLister_ResolvesEnabledSourceNodesAcrossFlows(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)

	flows := fakeFlows{
		{
			ID:      "triage",
			Enabled: true,
			Nodes: []flow.Node{
				{ID: "in-prs", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open"}},
				{ID: "off", Type: "github-source", Disabled: true, Config: &flow.GithubSourceConfig{Kind: "notifications"}},
				{ID: "sink", Type: "feed", Config: &flow.FeedConfig{}},
			},
		},
		// A disabled flow contributes no sources.
		{
			ID:      "paused",
			Enabled: false,
			Nodes:   []flow.Node{{ID: "in", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "notifications"}}},
		},
	}

	lister := NewFlowSourceLister(live, flows)
	sources, err := lister(context.Background())
	require.NoError(t, err)

	// Only the one enabled node in the one enabled flow, keyed flow-qualified.
	require.Len(t, sources, 1)
	require.Contains(t, sources, "triage/in-prs")
	gs, ok := sources["triage/in-prs"].(*githubSource)
	require.True(t, ok, "the lister should build githubSource instances")
	assert.Equal(t, "source:triage/in-prs", gs.topic)
}

// TestProducer_PrefetchesSearchSourcesInOneBatch preserves per-source
// snapshots while issuing one aliased GraphQL request for all search nodes.
func TestProducer_PrefetchesSearchSourcesInOneBatch(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)
	db := openTestPipelineDB(t)
	flows := fakeFlows{
		{
			ID:      "triage",
			Enabled: true,
			Nodes: []flow.Node{
				{ID: "issues", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open is:issue"}},
			},
		},
		{
			ID:      "reviews",
			Enabled: true,
			Nodes: []flow.Node{
				{ID: "prs", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open is:pr"}},
			},
		},
	}
	producer := ingest.NewProducer(db, NewFlowSourceLister(live, flows), time.Hour, nil, zerolog.Nop())
	producer.SetPrefetcher(live)

	producer.Tick(t.Context())

	assert.Equal(t, int32(1), api.calls.Load())
	assert.Equal(t, int32(2), api.aliases.Load())
	msgs, _, err := db.ReadFrom(t.Context(), 0, 10)
	require.NoError(t, err)
	topics := make(map[string]bool)
	for _, msg := range msgs {
		topics[msg.Topic] = true
	}
	assert.True(t, topics["source:triage/issues"])
	assert.True(t, topics["source:reviews/prs"])
}

// TestProducer_WithGithubSource_IngestsAsGithubNotGeneric is the guard for
// the ingest/sources split. IngestMetadata and SearchDef cross a package
// boundary now; if either stopped satisfying its ingest interface the type
// assertions in Producer.Tick would compile and silently never match, and
// every GitHub item would be ingested as SourceKind "generic" -- no GitHub
// classifier, no absence confirmation. Nothing else in the suite exercises
// that handoff across the boundary.
func TestProducer_WithGithubSource_IngestsAsGithubNotGeneric(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)
	db := openTestPipelineDB(t)

	flows := fakeFlows{{
		ID:      "triage",
		Enabled: true,
		Nodes: []flow.Node{
			{ID: "in-prs", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open is:pr"}},
		},
	}}

	producer := ingest.NewProducer(db, NewFlowSourceLister(live, flows), time.Hour, nil, zerolog.Nop())
	producer.SetSourceAdapter(NewGithubSourceAdapter(live))
	producer.Tick(t.Context())

	// source_kind alone does not prove it: Produce stamps "github" on every
	// Msg, so it survives the fallback. profile_id is the one field only
	// IngestMetadata supplies -- generic ingestion uses the flow-qualified
	// source id instead of the flow id.
	var sourceKind, profileID string
	require.NoError(t, db.Conn().QueryRowContext(t.Context(),
		`SELECT source_kind, profile_id FROM inbox_item`).Scan(&sourceKind, &profileID))
	assert.Equal(t, "github", sourceKind)
	assert.Equal(t, "triage", profileID, "IngestMetadata no longer satisfies ingest.MetadataSource")

	// The GitHub classifier ran rather than the generic fallback. Both call a
	// first observation "observed", so assert on what only the GitHub one
	// produces: its fixed summary, and a lifecycle read out of the payload's
	// state rather than left unknown.
	var summary, lifecycle string
	require.NoError(t, db.Conn().QueryRowContext(t.Context(),
		`SELECT e.summary, i.lifecycle FROM inbox_event e JOIN inbox_item i ON i.id = e.item_id ORDER BY e.id LIMIT 1`).
		Scan(&summary, &lifecycle))
	assert.Equal(t, "Added to workspace", summary, "the generic classifier ran instead of the GitHub one")
	assert.Equal(t, "active", lifecycle, "the GitHub classifier did not read the payload state")
}

// TestProducer_WithGithubSource_AppendsAcrossTicks is an end-to-end slice of
// the producer path: a real feed.LiveProvider fetching from a fake GitHub
// API, through a real githubSource, into a real store event log.
func TestProducer_WithGithubSource_AppendsAcrossTicks(t *testing.T) {
	t.Parallel()

	api := &singleSearchAPI{}
	live := newLiveProviderFixture(t, api)
	db := openTestPipelineDB(t)

	flows := fakeFlows{{
		ID:      "triage",
		Enabled: true,
		Nodes: []flow.Node{
			{ID: "in-prs", Type: "github-source", Config: &flow.GithubSourceConfig{Kind: "search", Query: "is:open is:pr author:@me"}},
		},
	}}

	var appendedOffsets []int64
	producer := ingest.NewProducer(db, NewFlowSourceLister(live, flows), 0, func(offset int64) {
		appendedOffsets = append(appendedOffsets, offset)
	}, zerolog.Nop())

	producer.Tick(context.Background())
	require.Len(t, appendedOffsets, 1)

	msgs, _, err := db.ReadFrom(context.Background(), 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "source:triage/in-prs", msgs[0].Topic)
	assert.Equal(t, "o/r#7", msgs[0].Key)
	assert.Len(t, msgs[1].Snapshot, 1)

	// A second tick with unchanged upstream data must not re-append (dedup)
	// even though githubSource re-emits the (cached) item every tick.
	producer.Tick(context.Background())
	msgs, _, err = db.ReadFrom(context.Background(), 0, 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 3, "unchanged items are deduplicated while every successful tick appends a snapshot")
	assert.Equal(t, int32(1), api.calls.Load(), "still one API request: the second tick's fetch was cache-served")
}
