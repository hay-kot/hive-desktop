package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

type fakeSessionLauncher struct {
	opts  dispatch.SessionLaunchOptions
	calls []dispatch.LaunchSessionRequest
	err   error
}

func (f *fakeSessionLauncher) LaunchSession(_ context.Context, req dispatch.LaunchSessionRequest) (dispatch.SessionExecutionOutcome, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return dispatch.SessionExecutionOutcome{}, f.err
	}
	return dispatch.SessionExecutionOutcome{ID: "session-1", Name: req.Name}, nil
}

func (f *fakeSessionLauncher) SessionLaunchOptions(context.Context) (dispatch.SessionLaunchOptions, error) {
	return f.opts, nil
}

// fakeSessionManager stands in for the hive seam. details is keyed by session
// id; sessions is what the list returns.
type fakeSessionManager struct {
	sessions []dispatch.SessionSummary
	statuses dispatch.SessionStatusSnapshot
	details  map[string]dispatch.SessionDetail
	risk     dispatch.SessionRisk
	err      error

	renamed   [][2]string
	deleted   []string
	recycled  []string
	pruned    int
	renameErr error
	spawned   [][3]string
	spawnErr  error
}

func (f *fakeSessionManager) ListSessions(context.Context) ([]dispatch.SessionSummary, error) {
	return f.sessions, f.err
}

func (f *fakeSessionManager) SessionStatuses(context.Context) (dispatch.SessionStatusSnapshot, error) {
	return f.statuses, f.err
}

func (f *fakeSessionManager) SessionDetail(_ context.Context, id string) (dispatch.SessionDetail, error) {
	detail, ok := f.details[id]
	if !ok {
		return dispatch.SessionDetail{}, errors.New("no such session")
	}
	return detail, nil
}

func (f *fakeSessionManager) SessionRisk(context.Context, string) (dispatch.SessionRisk, error) {
	return f.risk, f.err
}

func (f *fakeSessionManager) RenameSession(_ context.Context, id, name string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	f.renamed = append(f.renamed, [2]string{id, name})
	detail := f.details[id]
	detail.Name = name
	detail.Slug = dispatch.SlugifySessionName(name)
	f.details[id] = detail
	return nil
}

func (f *fakeSessionManager) DeleteSession(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

func (f *fakeSessionManager) RecycleSession(_ context.Context, id string) error {
	f.recycled = append(f.recycled, id)
	return f.err
}

func (f *fakeSessionManager) PruneSessions(context.Context) (int, error) {
	f.pruned++
	return 3, f.err
}

func (f *fakeSessionManager) SpawnTmuxSession(_ context.Context, name, path, repo string) error {
	if f.spawnErr != nil {
		return f.spawnErr
	}
	f.spawned = append(f.spawned, [3]string{name, path, repo})
	return nil
}

type fakeSessionTmux struct {
	renames [][2]string
	err     error
}

func (f *fakeSessionTmux) RenameSession(_ context.Context, from, to string) error {
	f.renames = append(f.renames, [2]string{from, to})
	return f.err
}

// fakeJobRunner runs the tracked function synchronously so tests can observe
// its outcome deterministically.
type fakeJobRunner struct {
	ran      bool
	label    string
	actionID string
	target   string
	err      error
}

func (f *fakeJobRunner) Track(ctx context.Context, label, actionID, target string, fn func(context.Context) error) int64 {
	f.ran = true
	f.label, f.actionID, f.target = label, actionID, target
	f.err = fn(ctx)
	return 7
}

func activeSession() (*fakeSessionManager, dispatch.SessionDetail) {
	detail := dispatch.SessionDetail{ID: "s1", Name: "review 81", Slug: "review-81", Repo: "acme/site", State: "active"}
	return &fakeSessionManager{
		sessions: []dispatch.SessionSummary{{ID: "s1", Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: "active"}},
		details:  map[string]dispatch.SessionDetail{"s1": detail},
	}, detail
}

func TestSessionsService_SessionLaunchOptions(t *testing.T) {
	expected := dispatch.SessionLaunchOptions{
		Repositories:      []dispatch.SessionLaunchRepository{{Name: "hive", Repository: "https://github.com/colonyops/hive.git"}},
		DefaultRepository: "https://github.com/colonyops/hive.git",
		Agents:            []string{"claude"},
		DefaultAgent:      "claude",
	}
	manager, _ := activeSession()
	svc := newSessionsService(&fakeSessionLauncher{opts: expected}, manager, &fakeSessionTmux{}, &fakeJobRunner{})
	got, err := svc.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestSessionsService_CreateSessionValidatesBeforeTracking(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	runner := &fakeJobRunner{}
	manager, _ := activeSession()
	svc := newSessionsService(launcher, manager, &fakeSessionTmux{}, runner)

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Name: "review", Prompt: "go"})
	assert.Equal(t, KindInvalid, KindOf(err), "repository is required")

	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r"})
	assert.Equal(t, KindInvalid, KindOf(err), "name is required")

	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "bad~name"})
	assert.Equal(t, KindInvalid, KindOf(err), "name must be valid")

	assert.False(t, runner.ran, "nothing is tracked until validation passes")
	assert.Empty(t, launcher.calls)
}

func TestSessionsService_CreateSessionLaunchesAsAJob(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	runner := &fakeJobRunner{}
	manager, _ := activeSession()
	svc := newSessionsService(launcher, manager, &fakeSessionTmux{}, runner)

	jobID, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{
		Repository: "  https://github.com/acme/site.git  ",
		Name:       "  review-81  ",
		Prompt:     "  fix it  ",
		Agent:      " claude ",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), jobID)
	assert.True(t, runner.ran)
	assert.Equal(t, "review-81", runner.target)
	require.NoError(t, runner.err)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, dispatch.LaunchSessionRequest{
		Name:   "review-81",
		Prompt: "fix it",
		Agent:  "claude",
		Repo:   "https://github.com/acme/site.git",
	}, launcher.calls[0])
}

func TestSessionsService_CreateSessionSurfacesDuplicateNameOnTheJob(t *testing.T) {
	runner := &fakeJobRunner{}
	manager, _ := activeSession()
	svc := newSessionsService(&fakeSessionLauncher{err: dispatch.ErrDuplicateSessionName}, manager, &fakeSessionTmux{}, runner)

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "dupe"})
	require.NoError(t, err, "a duplicate name is a job failure, not a validation error")
	require.Error(t, runner.err)
	assert.Contains(t, runner.err.Error(), "already exists")
}

func TestSessionsService_ListSessionsPassesEveryStateThrough(t *testing.T) {
	manager, _ := activeSession()
	manager.sessions = append(manager.sessions, dispatch.SessionSummary{ID: "s2", Name: "old", Slug: "old", State: "recycled"})
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})
	got, err := svc.ListSessions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, manager.sessions, got, "a recycled session is unattachable, not unmanageable")
}

func TestSessionsService_SessionStatusesPassesSnapshotThrough(t *testing.T) {
	manager, _ := activeSession()
	manager.statuses = dispatch.SessionStatusSnapshot{
		Items: []dispatch.SessionStatus{{
			SessionID: "s1",
			Running:   true,
			Windows:   []dispatch.SessionWindowStatus{{WindowID: "@1", Status: "ready", Tool: "codex"}},
		}},
		PollInterval: 1500 * time.Millisecond,
	}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	got, err := svc.SessionStatuses(t.Context())
	require.NoError(t, err)
	assert.Equal(t, manager.statuses, got)
}

func TestSessionsService_RenameSessionRenamesTmuxBeforeTheStore(t *testing.T) {
	manager, _ := activeSession()
	tmux := &fakeSessionTmux{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, tmux, &fakeJobRunner{})

	got, err := svc.RenameSession(t.Context(), "s1", "  Review 82  ")
	require.NoError(t, err)
	assert.Equal(t, "Review 82", got.Name)
	assert.Equal(t, "review-82", got.Slug, "the summary carries the new tmux target")
	assert.Equal(t, [][2]string{{"review-81", "review-82"}}, tmux.renames)
	assert.Equal(t, [][2]string{{"s1", "Review 82"}}, manager.renamed)
}

func TestSessionsService_RenameSessionLeavesTheStoreAloneWhenTmuxFails(t *testing.T) {
	manager, _ := activeSession()
	tmux := &fakeSessionTmux{err: errors.New("duplicate session: review-82")}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, tmux, &fakeJobRunner{})

	_, err := svc.RenameSession(t.Context(), "s1", "review 82")
	assert.Equal(t, KindConflict, KindOf(err))
	assert.Empty(t, manager.renamed, "the slug must not move without its tmux session")
}

func TestSessionsService_RenameSessionRollsTmuxBackWhenTheStoreFails(t *testing.T) {
	manager, _ := activeSession()
	manager.renameErr = errors.New("disk full")
	tmux := &fakeSessionTmux{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, tmux, &fakeJobRunner{})

	_, err := svc.RenameSession(t.Context(), "s1", "review 82")
	assert.Equal(t, KindInternal, KindOf(err))
	assert.Equal(t, [][2]string{{"review-81", "review-82"}, {"review-82", "review-81"}}, tmux.renames)
}

func TestSessionsService_RenameSessionRejectsASlugCollision(t *testing.T) {
	manager, _ := activeSession()
	manager.sessions = append(manager.sessions, dispatch.SessionSummary{ID: "s2", Name: "Review 82", Slug: "review-82"})
	tmux := &fakeSessionTmux{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, tmux, &fakeJobRunner{})

	// "review/82" slugifies onto s2's slug, which would give both sessions the
	// same tmux session name and the same directory slug.
	_, err := svc.RenameSession(t.Context(), "s1", "review/82")
	assert.Equal(t, KindInvalid, KindOf(err))
	assert.Empty(t, tmux.renames)
	assert.Empty(t, manager.renamed)
}

func TestSessionsService_RenameSessionValidatesTheName(t *testing.T) {
	manager, _ := activeSession()
	tmux := &fakeSessionTmux{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, tmux, &fakeJobRunner{})

	_, err := svc.RenameSession(t.Context(), "s1", "  ")
	assert.Equal(t, KindInvalid, KindOf(err))
	_, err = svc.RenameSession(t.Context(), "s1", "bad~name")
	assert.Equal(t, KindInvalid, KindOf(err))
	assert.Empty(t, tmux.renames)
}

func TestSessionsService_DeleteAndRecycleRunAsJobsLabelledWithTheSessionName(t *testing.T) {
	for _, tt := range []struct {
		name     string
		call     func(*SessionsService, context.Context) (int64, error)
		label    string
		actionID string
		seen     func(*fakeSessionManager) []string
	}{
		{"delete", func(s *SessionsService, ctx context.Context) (int64, error) { return s.DeleteSession(ctx, "s1") }, "Delete session", deleteSessionJobActionID, func(m *fakeSessionManager) []string { return m.deleted }},
		{"recycle", func(s *SessionsService, ctx context.Context) (int64, error) { return s.RecycleSession(ctx, "s1") }, "Recycle session", recycleSessionJobActionID, func(m *fakeSessionManager) []string { return m.recycled }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager, _ := activeSession()
			runner := &fakeJobRunner{}
			svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, runner)

			jobID, err := tt.call(svc, t.Context())
			require.NoError(t, err)
			assert.Equal(t, int64(7), jobID)
			assert.Equal(t, tt.label, runner.label)
			assert.Equal(t, tt.actionID, runner.actionID)
			assert.Equal(t, "review 81", runner.target, "the name is read before the session can stop existing")
			assert.Equal(t, []string{"s1"}, tt.seen(manager))
		})
	}
}

func TestSessionsService_DestructiveOperationsRejectAnUnknownSession(t *testing.T) {
	manager, _ := activeSession()
	runner := &fakeJobRunner{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, runner)

	_, err := svc.DeleteSession(t.Context(), "gone")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.RecycleSession(t.Context(), "gone")
	assert.Equal(t, KindNotFound, KindOf(err))
	assert.False(t, runner.ran)
}

func TestSessionsService_PruneRunsAsAJob(t *testing.T) {
	manager, _ := activeSession()
	runner := &fakeJobRunner{}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, runner)

	jobID, err := svc.PruneSessions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(7), jobID)
	assert.Equal(t, pruneSessionsJobActionID, runner.actionID)
	assert.Equal(t, 1, manager.pruned)
}

func TestSessionsService_SessionRiskCarriesTheWorktreeRecycleWarning(t *testing.T) {
	manager, _ := activeSession()
	manager.risk = dispatch.SessionRisk{UncommittedChanges: true, RecycleDeletes: true}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	risk, err := svc.SessionRisk(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, manager.risk, risk)

	_, err = svc.SessionRisk(t.Context(), " ")
	assert.Equal(t, KindInvalid, KindOf(err))
}

func TestSessionsService_SessionDetail(t *testing.T) {
	manager, detail := activeSession()
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	got, err := svc.SessionDetail(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, detail, got)

	_, err = svc.SessionDetail(t.Context(), "gone")
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestSessionsService_StartTmuxSessionSpawnsFromTheSessionsOwnCheckout(t *testing.T) {
	manager, detail := activeSession()
	detail.Path = "/repos/site-wt-ab12"
	manager.details["s1"] = detail
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	require.NoError(t, svc.StartTmuxSession(t.Context(), "review-81"))
	assert.Equal(t, [][3]string{{"review 81", "/repos/site-wt-ab12", "acme/site"}}, manager.spawned,
		"the spawn is hive's, so it gets the name, checkout and remote hive spawns from")
}

func TestSessionsService_StartTmuxSessionRejectsASlugNoSessionCarries(t *testing.T) {
	manager, _ := activeSession()
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	// A tmux session made by hand is attachable, but there is nothing to
	// create one from when it is gone.
	assert.Equal(t, KindNotFound, KindOf(svc.StartTmuxSession(t.Context(), "hand-rolled")))
	assert.Equal(t, KindInvalid, KindOf(svc.StartTmuxSession(t.Context(), "  ")))
	assert.Empty(t, manager.spawned)
}

func TestSessionsService_StartTmuxSessionRefusesASessionWithNoCheckout(t *testing.T) {
	recycled := dispatch.SessionDetail{ID: "s2", Name: "old", Slug: "old", Repo: "acme/site", State: "recycled"}
	manager := &fakeSessionManager{
		sessions: []dispatch.SessionSummary{{ID: "s2", Name: "old", Slug: "old", Repo: "acme/site", State: "recycled"}},
		details:  map[string]dispatch.SessionDetail{"s2": recycled},
	}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	assert.Equal(t, KindConflict, KindOf(svc.StartTmuxSession(t.Context(), "old")))
	assert.Empty(t, manager.spawned, "a recycled session's directory is gone; a terminal in it would be one too")
}

func TestSessionsService_StartTmuxSessionRefusesASlugItsNameWouldNotSpawn(t *testing.T) {
	// Hive spawns under the slug it derives from the name, so a record whose two
	// have drifted apart would create a session under a name nothing attaches to.
	drifted := dispatch.SessionDetail{ID: "s1", Name: "review 82", Slug: "review-81", Repo: "acme/site", State: "active"}
	manager := &fakeSessionManager{
		sessions: []dispatch.SessionSummary{{ID: "s1", Name: "review 82", Slug: "review-81", Repo: "acme/site", State: "active"}},
		details:  map[string]dispatch.SessionDetail{"s1": drifted},
	}
	svc := newSessionsService(&fakeSessionLauncher{}, manager, &fakeSessionTmux{}, &fakeJobRunner{})

	assert.Equal(t, KindConflict, KindOf(svc.StartTmuxSession(t.Context(), "review-81")))
	assert.Empty(t, manager.spawned)
}

func TestSessionsService_UnavailableWithoutDependencies(t *testing.T) {
	svc := newSessionsService(nil, nil, nil, &fakeJobRunner{})
	assert.Equal(t, KindUnavailable, KindOf(svc.StartTmuxSession(t.Context(), "review-81")))
	_, err := svc.SessionLaunchOptions(t.Context())
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "n"})
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.ListSessions(t.Context())
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.SessionDetail(t.Context(), "s1")
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.SessionRisk(t.Context(), "s1")
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.RenameSession(t.Context(), "s1", "n")
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.DeleteSession(t.Context(), "s1")
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.RecycleSession(t.Context(), "s1")
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.PruneSessions(t.Context())
	assert.Equal(t, KindUnavailable, KindOf(err))
}
