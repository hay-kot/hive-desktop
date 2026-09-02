package app

import (
	"context"
	"os"
	"path/filepath"
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
	svc       *SchedulesService
	db        *store.DB
	root      string
	launcher  *fakeScheduleLauncher
	published *[]string
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

	published := []string{}
	svc := newSchedulesService(workspaces, db, scheduler, func(workspace string) {
		published = append(published, workspace)
	})
	return scheduleFixture{svc: svc, db: db, root: root, launcher: launcher, published: &published}
}

func (f scheduleFixture) manifest(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.root, "demo", "agent-workspace.yaml"))
	require.NoError(t, err)
	return string(raw)
}

func TestSchedulesServiceSaveWritesTheManifestAndListsTheNextRun(t *testing.T) {
	f := newTestSchedulesService(t)

	saved, err := f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "demo", ID: "weekly", Name: "Weekly summary",
		Cron: "0 9 * * 5", Prompt: "Summarize the week.",
	})
	require.NoError(t, err)
	assert.Equal(t, "weekly", saved.ID)
	assert.Equal(t, "Weekly summary", saved.Name)
	assert.Equal(t, "run", saved.OnMissed, "the manifest omits the default; the view names it")
	require.NotNil(t, saved.NextRunAt)
	assert.Greater(t, *saved.NextRunAt, time.Now().UnixMilli())
	assert.Nil(t, saved.LastRun, "a schedule that has never fired has no last run")
	assert.Equal(t, []string{"demo"}, *f.published)

	assert.Contains(t, f.manifest(t), "id: weekly")
	assert.NotContains(t, f.manifest(t), "on_missed", "a default is not written")

	listed, err := f.svc.List(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, saved.ID, listed[0].ID)
	assert.Equal(t, "0 9 * * 5", listed[0].Cron)

	// A disabled schedule keeps its cron but has nothing coming.
	disabled, err := f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "demo", ID: "weekly", Name: "Weekly summary",
		Cron: "0 9 * * 5", Prompt: "Summarize the week.", Disabled: true, OnMissed: "skip",
	})
	require.NoError(t, err)
	assert.True(t, disabled.Disabled)
	assert.Equal(t, "skip", disabled.OnMissed)
	assert.Nil(t, disabled.NextRunAt)
}

func TestSchedulesServiceSaveRejectsBadInput(t *testing.T) {
	f := newTestSchedulesService(t)

	_, err := f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "demo", ID: "weekly", Cron: "not a cron", Prompt: "hi",
	})
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
	assert.Contains(t, err.Error(), "not a cron", "the reason has to reach the editor")

	_, err = f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "missing", ID: "weekly", Cron: "@daily", Prompt: "hi",
	})
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	assert.NotContains(t, f.manifest(t), "schedules", "a rejected edit writes nothing")
}

func TestSchedulesServiceDeleteDropsTheEntryAndItsCursor(t *testing.T) {
	f := newTestSchedulesService(t)

	_, err := f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "demo", ID: "weekly", Cron: "@daily", Prompt: "hi",
	})
	require.NoError(t, err)
	require.NoError(t, f.db.UpsertScheduleCursor(t.Context(), store.ScheduleCursorRecord{
		Workspace: "demo", ScheduleID: "weekly", EvaluatedThrough: time.Now().UnixMilli(), Cron: "@daily",
	}))

	require.NoError(t, f.svc.Delete(t.Context(), "demo", "weekly"))

	assert.NotContains(t, f.manifest(t), "schedules", "the key goes with the last entry")
	_, ok, err := f.db.GetScheduleCursor(t.Context(), "demo", "weekly")
	require.NoError(t, err)
	assert.False(t, ok, "a reused id must start from now, not from the deleted schedule's window")

	listed, err := f.svc.List(t.Context(), "demo")
	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestSchedulesServiceRunNowRecordsAManualRun(t *testing.T) {
	f := newTestSchedulesService(t)

	_, err := f.svc.Save(t.Context(), ScheduleEdit{
		Workspace: "demo", ID: "weekly", Name: "Weekly summary",
		Cron: "0 9 * * 5", Prompt: "Summarize {{ .Workspace.Name }}.",
	})
	require.NoError(t, err)

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
	listed, err := f.svc.List(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NotNil(t, listed[0].LastRun)
	assert.Equal(t, run.ID, listed[0].LastRun.ID)

	_, err = f.svc.RunNow(t.Context(), "demo", "nope")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	// A manual run never consumes the window, so nothing is left behind that
	// would cancel the next real occurrence.
	_, ok, err := f.db.GetScheduleCursor(t.Context(), "demo", "weekly")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSchedulesServiceRunsSpansTheWorkspaceNewestFirst(t *testing.T) {
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

	all, err := f.svc.Runs(t.Context(), "demo", "", 0)
	require.NoError(t, err)
	require.Len(t, all, 4)
	for i := 1; i < len(all); i++ {
		assert.GreaterOrEqual(t, all[i-1].StartedAt, all[i].StartedAt, "runs come back newest first")
	}

	scoped, err := f.svc.Runs(t.Context(), "demo", "alpha", 0)
	require.NoError(t, err)
	require.Len(t, scoped, 2)
	for _, run := range scoped {
		assert.Equal(t, "alpha", run.ScheduleID)
	}

	limited, err := f.svc.Runs(t.Context(), "demo", "", 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)
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
