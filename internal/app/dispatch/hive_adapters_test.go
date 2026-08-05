package dispatch

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/colonyops/hive/pkg/executil"
	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/config"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/eventbus"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/messaging"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	coreterminal "github.com/hay-kot/hive-desktop/internal/hivecore/core/terminal"
	coredb "github.com/hay-kot/hive-desktop/internal/hivecore/data/db"
	"github.com/hay-kot/hive-desktop/internal/hivecore/data/stores"
	hivesvc "github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type durableMessageServiceTest struct {
	message messaging.Message
	topics  []string
}

func (s *durableMessageServiceTest) Publish(_ context.Context, message messaging.Message, topics []string) (messaging.PublishResult, error) {
	s.message, s.topics = message, topics
	return messaging.PublishResult{Topics: topics}, nil
}

func TestHiveMessagePublisherUsesFixedSenderEmptySessionAndLiteralTopic(t *testing.T) {
	service := &durableMessageServiceTest{}
	topic, err := NewHiveMessagePublisher(service).PublishMessage(t.Context(), "hello", "agent.session.inbox")
	require.NoError(t, err)
	assert.Equal(t, "agent.session.inbox", topic)
	assert.Equal(t, messaging.Message{Payload: "hello", Sender: "hive-desktop", SessionID: ""}, service.message)
	assert.Equal(t, []string{"agent.session.inbox"}, service.topics)
}

func TestHiveMessagePublisherPersistsThroughCoreSQLiteReopen(t *testing.T) {
	dir := t.TempDir()
	first, err := coredb.Open(dir, coredb.DefaultOpenOptions())
	require.NoError(t, err)
	service := hivesvc.NewMessageService(stores.NewMessageStore(first, 0), &config.Config{}, eventbus.New(8))
	publisher := NewHiveMessagePublisher(service)
	const topic = "agent.session.inbox"
	published, err := publisher.PublishMessage(t.Context(), "hello from desktop", topic)
	require.NoError(t, err)
	assert.Equal(t, topic, published)
	require.NoError(t, first.Close())

	reopened, err := coredb.Open(dir, coredb.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	reopenedService := hivesvc.NewMessageService(stores.NewMessageStore(reopened, 0), &config.Config{}, eventbus.New(8))
	messages, err := reopenedService.Subscribe(t.Context(), topic, time.Time{})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "hello from desktop", messages[0].Payload)
	assert.Equal(t, "hive-desktop", messages[0].Sender)
	assert.Empty(t, messages[0].SessionID)
	assert.Equal(t, topic, messages[0].Topic)
}

// newHiveSessions builds the real vendored session service over a temporary
// core database. It is what makes these tests worth having: the seam's job is
// to hold against hive's actual implementation, so SessionManagement is
// satisfied structurally here rather than by a fake shaped to fit it.
func newHiveSessions(t *testing.T) (*HiveSessionManager, session.Store) {
	t.Helper()
	return newHiveSessionsWith(t, &config.Config{}, &executil.RealExecutor{})
}

func newHiveSessionsWith(t *testing.T, cfg *config.Config, exec executil.Executor) (*HiveSessionManager, session.Store) {
	t.Helper()
	database, err := coredb.Open(t.TempDir(), coredb.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	store := stores.NewSessionStore(database)
	svc := hivesvc.NewSessionService(
		store,
		git.NewExecutor("git", exec),
		cfg,
		eventbus.New(8),
		exec,
		tmpl.New(tmpl.Config{}),
		zerolog.Nop(),
		io.Discard,
		io.Discard,
	)
	return NewHiveSessionManager(svc, nil, 0), store
}

type listingSessionManagement struct {
	SessionManagement
	sessions []session.Session
}

func (m listingSessionManagement) ListSessions(context.Context) ([]session.Session, error) {
	return m.sessions, nil
}

type fakeSessionStatusSource struct {
	available bool
	results   map[string]hivesvc.TerminalStatus
	seen      []*session.Session
}

func (f *fakeSessionStatusSource) Available() bool { return f.available }

func (f *fakeSessionStatusSource) FetchBatch(_ context.Context, sessions []*session.Session, _ []hivesvc.RootRepoTarget) map[string]hivesvc.TerminalStatus {
	f.seen = sessions
	return f.results
}

// recordingExecutor stands in for the shell hive spawns tmux through, so the
// seam can be checked against the real vendored spawner with no tmux server
// running. absent fails has-session, which is how tmux answers for a session it
// does not hold.
type recordingExecutor struct {
	absent bool
	runs   [][]string
}

func (e *recordingExecutor) record(cmd string, args []string) error {
	e.runs = append(e.runs, append([]string{cmd}, args...))
	if e.absent && len(args) > 0 && args[0] == "has-session" {
		return errors.New("can't find session")
	}
	return nil
}

func (e *recordingExecutor) Run(_ context.Context, cmd string, args ...string) ([]byte, error) {
	return nil, e.record(cmd, args)
}

func (e *recordingExecutor) RunDir(_ context.Context, _, cmd string, args ...string) ([]byte, error) {
	return nil, e.record(cmd, args)
}

func (e *recordingExecutor) RunStream(_ context.Context, _, _ io.Writer, cmd string, args ...string) error {
	return e.record(cmd, args)
}

func (e *recordingExecutor) RunDirStream(_ context.Context, _ string, _, _ io.Writer, cmd string, args ...string) error {
	return e.record(cmd, args)
}

// spawnConfig is a rule whose windows are distinguishable from hive's defaults,
// so the test can tell "hive rendered the configured spawn" from "something here
// built a window set of its own".
func spawnConfig() *config.Config {
	return &config.Config{Rules: []config.Rule{{
		Windows: []config.WindowConfig{
			{Name: "agent", Command: "run {{ .Slug }}", Focus: true},
			{Name: "shell"},
		},
	}}}
}

func TestHiveSessionManagerSpawnsTheConfiguredWindowsDetached(t *testing.T) {
	exec := &recordingExecutor{absent: true}
	manager, _ := newHiveSessionsWith(t, spawnConfig(), exec)

	require.NoError(t, manager.SpawnTmuxSession(t.Context(), "review 81", "/tmp/review-81", "acme/site"))

	assert.Contains(t, exec.runs, []string{"tmux", "has-session", "-t", "review-81"})
	// The session is created under the slug, in the session's own directory,
	// running the configured command — hive's spawn semantics, not a second
	// definition of them here.
	assert.Contains(t, exec.runs, []string{"tmux", "new-session", "-d", "-s", "review-81", "-n", "agent", "-c", "/tmp/review-81", "--", "sh", "-c", "run review-81"})
	assert.Contains(t, exec.runs, []string{"tmux", "new-window", "-t", "review-81", "-n", "shell", "-c", "/tmp/review-81"})
	for _, run := range exec.runs {
		assert.NotContains(t, run, "attach-session", "the desktop attaches over control mode; the spawn must stay detached")
		assert.NotContains(t, run, "switch-client")
	}
}

func TestHiveSessionManagerSpawnLeavesALiveSessionAlone(t *testing.T) {
	exec := &recordingExecutor{}
	manager, _ := newHiveSessionsWith(t, spawnConfig(), exec)

	require.NoError(t, manager.SpawnTmuxSession(t.Context(), "review 81", "/tmp/review-81", "acme/site"))

	assert.Equal(t, [][]string{{"tmux", "has-session", "-t", "review-81"}}, exec.runs,
		"a session tmux already holds is not respawned, so every cold attach can ask for one")
}

func TestHiveSessionManagerListsEveryState(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC().Truncate(time.Second)

	active := session.Session{ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81", Remote: "acme/site", State: session.StateActive, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.Save(t.Context(), active))
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s2", Name: "old", Slug: "old", Path: "/tmp/old", State: session.StateRecycled, CreatedAt: now, UpdatedAt: now}))

	got, err := manager.ListSessions(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []SessionSummary{
		{ID: "s1", Name: "review 81", Slug: "review-81", Repo: "acme/site", State: "active"},
		{ID: "s2", Name: "old", Slug: "old", State: "recycled"},
	}, got)
}

func TestHiveSessionManagerProjectsLiveStatusForActiveSessions(t *testing.T) {
	statuses := &fakeSessionStatusSource{
		available: true,
		results: map[string]hivesvc.TerminalStatus{
			"s1": {Running: true, Windows: []hivesvc.WindowStatus{
				{WindowID: "@1", Status: coreterminal.StatusApproval, Tool: "claude"},
				{WindowID: "@2", Status: coreterminal.StatusActive, Tool: "pi"},
			}},
			"s2": {Running: true, WindowID: "@3", Status: coreterminal.StatusReady, Tool: "codex"},
			"s4": {Status: coreterminal.StatusMissing, Error: errors.New("session disappeared")},
		},
	}
	manager := NewHiveSessionManager(listingSessionManagement{sessions: []session.Session{
		{ID: "s1", State: session.StateActive},
		{ID: "s2", State: session.StateActive},
		{ID: "s3", State: session.StateRecycled},
		{ID: "s4", State: session.StateActive},
	}}, statuses, 1750*time.Millisecond)

	got, err := manager.SessionStatuses(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1750*time.Millisecond, got.PollInterval)
	assert.Equal(t, []SessionStatus{
		{SessionID: "s1", Running: true, Windows: []SessionWindowStatus{
			{WindowID: "@1", Status: "approval", Tool: "claude"},
			{WindowID: "@2", Status: "active", Tool: "pi"},
		}},
		{SessionID: "s2", Running: true, Windows: []SessionWindowStatus{{WindowID: "@3", Status: "ready", Tool: "codex"}}},
		{SessionID: "s4", Windows: []SessionWindowStatus{}},
	}, got.Items)
	require.Len(t, statuses.seen, 3)
	assert.Equal(t, "s1", statuses.seen[0].ID)
	assert.Equal(t, "s2", statuses.seen[1].ID)
	assert.Equal(t, "s4", statuses.seen[2].ID)
}

// An inbox item asks about the one or two sessions it spawned, so the probe
// must be scoped to those — a full sweep would pay a tmux round trip for every
// active session in the install to answer it.
func TestHiveSessionManagerRunningSessionsProbesOnlyTheNamedActiveSessions(t *testing.T) {
	statuses := &fakeSessionStatusSource{
		available: true,
		results:   map[string]hivesvc.TerminalStatus{"s1": {Running: true}},
	}
	manager := NewHiveSessionManager(listingSessionManagement{sessions: []session.Session{
		{ID: "s1", State: session.StateActive},
		{ID: "s2", State: session.StateRecycled},
		{ID: "s3", State: session.StateActive},
	}}, statuses, time.Second)

	got, err := manager.RunningSessions(t.Context(), []string{"s1", "s2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"s1": true}, got)
	// s2 is recycled and s3 was not asked about, so neither is probed.
	require.Len(t, statuses.seen, 1)
	assert.Equal(t, "s1", statuses.seen[0].ID)
}

// Terminal mode ships dark, so this is the default install: no liveness source
// is data, not a failure — nothing reads as running and the caller still gets
// its sessions.
func TestHiveSessionManagerRunningSessionsReportsNothingWhenTerminalUnavailable(t *testing.T) {
	manager := NewHiveSessionManager(listingSessionManagement{sessions: []session.Session{
		{ID: "s1", State: session.StateActive},
	}}, &fakeSessionStatusSource{}, time.Second)

	got, err := manager.RunningSessions(t.Context(), []string{"s1"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHiveSessionManagerReturnsEmptyStatusWhenTerminalUnavailable(t *testing.T) {
	manager := NewHiveSessionManager(listingSessionManagement{}, &fakeSessionStatusSource{}, 1500*time.Millisecond)

	got, err := manager.SessionStatuses(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got.Items)
	assert.Equal(t, 1500*time.Millisecond, got.PollInterval)
}

func TestHiveSessionManagerDetailReadsWorktreeMetadata(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC().Truncate(time.Second)

	sess := session.Session{
		ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81",
		Remote: "acme/site", State: session.StateActive, CloneStrategy: session.CloneStrategyWorktree,
		Tags: []string{"pr-81"}, CreatedAt: now, UpdatedAt: now,
	}
	sess.SetMeta(session.MetaWorktreeBranch, "hive/review-81")
	require.NoError(t, store.Save(t.Context(), sess))

	detail, err := manager.SessionDetail(t.Context(), "s1")
	require.NoError(t, err)
	// Timestamps come back in the local zone, so they are compared by instant.
	assert.True(t, detail.CreatedAt.Equal(now))
	assert.True(t, detail.UpdatedAt.Equal(now))
	detail.CreatedAt, detail.UpdatedAt = now, now
	assert.Equal(t, SessionDetail{
		ID: "s1", Name: "review 81", Slug: "review-81", Repo: "acme/site", State: "active",
		Path: "/tmp/review-81", CloneStrategy: session.CloneStrategyWorktree,
		WorktreeBranch: "hive/review-81", Tags: []string{"pr-81"}, CreatedAt: now, UpdatedAt: now,
	}, detail)

	_, err = manager.SessionDetail(t.Context(), "missing")
	require.Error(t, err)
}

func TestHiveSessionManagerRenameReSlugsTheSession(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC()
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81", State: session.StateActive, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, manager.RenameSession(t.Context(), "s1", "Review 82"))

	detail, err := manager.SessionDetail(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "Review 82", detail.Name)
	// The slug moving is the hazard the desktop compensates for: it is the tmux
	// session name, and hive renames the record without renaming tmux. See
	// app.SessionsService.RenameSession and ADR terminal-atlas-renderer.
	assert.Equal(t, "review-82", detail.Slug)
	assert.Equal(t, "/tmp/review-81", detail.Path, "the directory keeps the slug it was cloned under")
}

func TestHiveSessionManagerRiskIsEmptyForANonActiveSession(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC()
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s1", Name: "old", Slug: "old", Path: "/tmp/old", State: session.StateRecycled, CloneStrategy: session.CloneStrategyWorktree, CreatedAt: now, UpdatedAt: now}))

	risk, err := manager.SessionRisk(t.Context(), "s1")
	require.NoError(t, err)
	// No live clone left to hold unsaved work — but recycling a worktree
	// session still deletes it, which the confirmation has to say.
	assert.Equal(t, SessionRisk{RecycleDeletes: true}, risk)
}
