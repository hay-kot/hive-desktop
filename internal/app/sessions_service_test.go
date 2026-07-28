package app

import (
	"context"
	"testing"

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

// fakeJobRunner runs the tracked function synchronously so tests can observe
// its outcome deterministically.
type fakeJobRunner struct {
	ran    bool
	target string
	err    error
}

func (f *fakeJobRunner) Track(ctx context.Context, _, _, target string, fn func(context.Context) error) int64 {
	f.ran = true
	f.target = target
	f.err = fn(ctx)
	return 7
}

func TestSessionsService_SessionLaunchOptions(t *testing.T) {
	expected := dispatch.SessionLaunchOptions{
		Repositories:      []dispatch.SessionLaunchRepository{{Name: "hive", Repository: "https://github.com/colonyops/hive.git"}},
		DefaultRepository: "https://github.com/colonyops/hive.git",
		Agents:            []string{"claude"},
		DefaultAgent:      "claude",
	}
	svc := newSessionsService(&fakeSessionLauncher{opts: expected}, &fakeJobRunner{})
	got, err := svc.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestSessionsService_CreateSessionValidatesBeforeTracking(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	runner := &fakeJobRunner{}
	svc := newSessionsService(launcher, runner)

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
	svc := newSessionsService(launcher, runner)

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
	svc := newSessionsService(&fakeSessionLauncher{err: dispatch.ErrDuplicateSessionName}, runner)

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "dupe"})
	require.NoError(t, err, "a duplicate name is a job failure, not a validation error")
	require.Error(t, runner.err)
	assert.Contains(t, runner.err.Error(), "already exists")
}

func TestSessionsService_UnavailableWithoutDependencies(t *testing.T) {
	svc := newSessionsService(nil, &fakeJobRunner{})
	_, err := svc.SessionLaunchOptions(t.Context())
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "n"})
	assert.Equal(t, KindUnavailable, KindOf(err))
}
