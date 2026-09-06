package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/schedule"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// fakeScheduleLauncher stands in for the agent-workspace service: launching a
// chat for real needs tmux and an agent CLI, and neither says anything about
// what this service does with the run it gets back.
type fakeScheduleLauncher struct {
	mu       sync.Mutex
	launched []schedule.LaunchRequest
	nextID   int64
	live     bool
	err      error
}

func (f *fakeScheduleLauncher) Launch(_ context.Context, req schedule.LaunchRequest) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.launched = append(f.launched, req)
	f.nextID++
	return f.nextID, nil
}

func (f *fakeScheduleLauncher) SessionLive(context.Context, int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live, nil
}

func (f *fakeScheduleLauncher) requests() []schedule.LaunchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]schedule.LaunchRequest(nil), f.launched...)
}

type scheduleFixture struct {
	svc        *SchedulesService
	db         *store.DB
	root       string
	workspaces *agentws.Store
	scheduler  *schedule.Scheduler
	launcher   *fakeScheduleLauncher
}

// newTestSchedulesService builds the service over a real workspace root and a
// real database, with only the launcher faked. The scheduler is the real one:
// RunNow's whole behaviour lives in it, and a fake would prove nothing about
// the run this service hands back.
func newTestSchedulesService(t *testing.T) scheduleFixture {
	t.Helper()

	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")

	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	workspaces := agentws.NewStore(root)
	require.NoError(t, workspaces.Reload())

	launcher := &fakeScheduleLauncher{}
	adapters := scheduleWorkspaces{store: workspaces}
	scheduler := schedule.New(schedule.Options{
		Source:   adapters,
		Names:    adapters,
		Store:    scheduleStore{db: db},
		Launcher: launcher,
		Logger:   zerolog.Nop(),
	})

	svc := newSchedulesService(workspaces, db, scheduler, zerolog.Nop())
	return scheduleFixture{
		svc: svc, db: db, root: root, workspaces: workspaces,
		scheduler: scheduler, launcher: launcher,
	}
}

// declare puts specs in the manifest the way the workspace editor does, then
// reloads the readers of it. This service only reads schedules; writing one is
// AgentWorkspacesService's.
func (f scheduleFixture) declare(t *testing.T, specs ...schedule.Spec) {
	t.Helper()
	require.NoError(t, agentws.WriteManifest(f.root, "demo", agentws.ManifestEdit{
		Name: "Demo", Agent: "claude", Autonomy: agentws.AutonomyAsk, Schedules: specs,
	}))
	require.NoError(t, f.workspaces.Reload())
	f.scheduler.Reload()
}

// rows reads the demo workspace's schedules back the way the workspace view
// does: the manifest's specs joined with their run state.
func (f scheduleFixture) rows(t *testing.T) []ScheduleView {
	t.Helper()
	for _, st := range f.workspaces.Statuses() {
		if st.Dir != "demo" {
			continue
		}
		require.True(t, st.Valid, st.Err)
		return scheduleRows(t.Context(), f.db, zerolog.Nop(), st.Workspace.Schedules, time.Now())
	}
	t.Fatal("the demo workspace is missing")
	return nil
}

// List is the MCP tools' read. A workspace that is missing is not_found; one
// whose manifest does not parse is refused with the reason rather than
// answered with rows the file no longer says.
func TestSchedulesServiceListReadsAWorkspaceItCanParse(t *testing.T) {
	f := newTestSchedulesService(t)
	f.declare(t, schedule.Spec{ID: "weekly", Name: "Weekly summary", Cron: "0 9 * * 5", Prompt: "go"})

	listed, err := f.svc.List(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "weekly", listed[0].ID)
	require.NotNil(t, listed[0].NextRunAt)

	_, err = f.svc.List(t.Context(), "missing")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	writeAgentWorkspaceManifest(t, f.root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\nschedules:\n  - id: weekly\n    cron: not a cron\n    prompt: go\n")
	require.NoError(t, f.workspaces.Reload())
	_, err = f.svc.List(t.Context(), "demo")
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func TestScheduleRowsJoinTheNextRun(t *testing.T) {
	f := newTestSchedulesService(t)

	f.declare(t, schedule.Spec{
		ID: "weekly", Name: "Weekly summary", Cron: "0 9 * * 5", Prompt: "Summarize the week.",
	})

	rows := f.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "weekly", rows[0].ID)
	assert.Equal(t, "Weekly summary", rows[0].Name)
	assert.Equal(t, "0 9 * * 5", rows[0].Cron)
	assert.Equal(t, "run", rows[0].OnMissed, "the manifest omits the default; the view names it")
	require.NotNil(t, rows[0].NextRunAt)
	assert.Greater(t, *rows[0].NextRunAt, time.Now().UnixMilli())
	assert.Nil(t, rows[0].LastRun, "a schedule that has never fired has no last run")

	// A disabled schedule keeps its cron but has nothing coming.
	f.declare(t, schedule.Spec{
		ID: "weekly", Name: "Weekly summary", Cron: "0 9 * * 5", Prompt: "Summarize the week.",
		Disabled: true, OnMissed: schedule.OnMissedSkip,
	})
	rows = f.rows(t)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Disabled)
	assert.Equal(t, "skip", rows[0].OnMissed)
	assert.Nil(t, rows[0].NextRunAt)
}

// The run history is a decoration on a row the manifest already fully
// describes, so a database that cannot answer for it costs the row its
// lastRun, not the caller its listing.
func TestScheduleRowsSurviveAnUnreadableRunHistory(t *testing.T) {
	f := newTestSchedulesService(t)

	f.declare(t, schedule.Spec{ID: "weekly", Name: "Weekly summary", Cron: "@daily", Prompt: "go"})
	require.NoError(t, f.db.Close())

	rows := f.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "weekly", rows[0].ID)
	require.NotNil(t, rows[0].NextRunAt, "everything the manifest says is still there")
	assert.Nil(t, rows[0].LastRun)
}

func TestSchedulesServiceRunNowRecordsAManualRun(t *testing.T) {
	f := newTestSchedulesService(t)

	f.declare(t, schedule.Spec{
		ID: "weekly", Name: "Weekly summary",
		Cron: "0 9 * * 5", Prompt: "Summarize {{ .Workspace.Name }}.",
	})

	run, err := f.svc.RunNow(t.Context(), "demo", "weekly")
	require.NoError(t, err)
	assert.Equal(t, "manual", run.Reason)
	assert.Equal(t, "launched", run.Status)
	assert.Equal(t, "Summarize Demo.", run.Prompt, "the workspace name comes from the manifest, not the directory")
	require.NotNil(t, run.SessionID)
	assert.Equal(t, int64(1), *run.SessionID)

	requests := f.launcher.requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "demo", requests[0].Workspace)
	assert.Contains(t, requests[0].Name, "Weekly summary")

	// The manual run is the schedule's newest, so it shows as its last run.
	rows := f.rows(t)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].LastRun)
	assert.Equal(t, run.ID, rows[0].LastRun.ID)

	_, err = f.svc.RunNow(t.Context(), "demo", "nope")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	// A manual run never consumes the window, so nothing is left behind that
	// would cancel the next real occurrence.
	_, ok, err := f.db.GetScheduleCursor(t.Context(), "demo", "weekly")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSchedulesServiceRunsAreOneSchedulesNewestFirst(t *testing.T) {
	f := newTestSchedulesService(t)

	for i, id := range []string{"alpha", "beta"} {
		for n := range 2 {
			_, err := f.db.InsertScheduleRun(t.Context(), store.ScheduleRunRecord{
				Workspace: "demo", ScheduleID: id, ScheduleName: id,
				ScheduledFor: int64(1_000 + i*10 + n), StartedAt: int64(1_000 + i*10 + n),
				Reason: "due", Status: "launched", SessionID: int64(1 + n),
			})
			require.NoError(t, err)
		}
	}

	scoped, err := f.svc.Runs(t.Context(), "demo", "alpha", 0)
	require.NoError(t, err)
	require.Len(t, scoped, 2)
	assert.Greater(t, scoped[0].StartedAt, scoped[1].StartedAt, "runs come back newest first")
	for _, run := range scoped {
		assert.Equal(t, "alpha", run.ScheduleID)
	}

	limited, err := f.svc.Runs(t.Context(), "demo", "alpha", 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	// History is per schedule, and a schedule that no longer exists still
	// answers with what it did.
	gone, err := f.svc.Runs(t.Context(), "demo", "beta", 0)
	require.NoError(t, err)
	assert.Len(t, gone, 2)

	_, err = f.svc.Runs(t.Context(), "demo", "", 0)
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
}

// A run points at the chat it launched only while that chat exists: a
// scheduled chat deletes itself when its task is done, and a history entry
// must not offer to open a chat nobody can.
func TestSchedulesServiceRunsDropAChatThatWasDeleted(t *testing.T) {
	f := newTestSchedulesService(t)
	f.declare(t, schedule.Spec{ID: "alpha", Cron: "@daily", Prompt: "go"})

	chat, err := f.db.CreateAgentWorkspaceSession(t.Context(), store.AgentWorkspaceSession{
		Workspace: "demo", Name: "s1", Agent: "claude", AgentSessionID: "a", CreatedAt: 1, LastOpenedAt: 1,
	})
	require.NoError(t, err)
	for _, run := range []store.ScheduleRunRecord{
		{Workspace: "demo", ScheduleID: "alpha", ScheduleName: "alpha", ScheduledFor: 100, StartedAt: 100, Reason: "due", Status: "launched", SessionID: chat.ID},
		{Workspace: "demo", ScheduleID: "alpha", ScheduleName: "alpha", ScheduledFor: 200, StartedAt: 200, Reason: "due", Status: "launched", SessionID: chat.ID + 1000},
	} {
		_, err := f.db.InsertScheduleRun(t.Context(), run)
		require.NoError(t, err)
	}

	runs, err := f.svc.Runs(t.Context(), "demo", "alpha", 0)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Nil(t, runs[0].SessionID, "the newest run's chat is gone")
	require.NotNil(t, runs[1].SessionID)
	assert.Equal(t, chat.ID, *runs[1].SessionID, "a chat that still exists is still pointed at")

	rows := f.rows(t)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].LastRun)
	assert.Nil(t, rows[0].LastRun.SessionID)
}

func TestSchedulesServicePreviewReportsErrorsAsFields(t *testing.T) {
	f := newTestSchedulesService(t)

	preview, err := f.svc.Preview(t.Context(), PreviewRequest{
		Workspace: "demo", Cron: "0 9 * * 5", Prompt: "Summarize {{ .Workspace.Name }} since {{ date \"2006-01-02\" .LastRun }}.",
	})
	require.NoError(t, err)
	assert.Len(t, preview.Next, previewOccurrences)
	assert.Empty(t, preview.CronError)
	assert.Empty(t, preview.PromptError)
	assert.Contains(t, preview.Prompt, "Summarize Demo since ")

	broken, err := f.svc.Preview(t.Context(), PreviewRequest{
		Workspace: "demo", Cron: "every friday", Prompt: "{{ .Nope }}",
	})
	require.NoError(t, err, "a bad edit is the answer, not a failure")
	assert.NotEmpty(t, broken.CronError)
	assert.NotEmpty(t, broken.PromptError)
	assert.Empty(t, broken.Next)
	assert.Empty(t, broken.Prompt)
}
