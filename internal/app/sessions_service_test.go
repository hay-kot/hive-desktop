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

func TestSessionsService_SessionLaunchOptions(t *testing.T) {
	expected := dispatch.SessionLaunchOptions{
		Repositories:      []dispatch.SessionLaunchRepository{{Name: "hive", Repository: "https://github.com/colonyops/hive.git"}},
		DefaultRepository: "https://github.com/colonyops/hive.git",
		Agents:            []string{"claude"},
		DefaultAgent:      "claude",
	}
	svc := newSessionsService(&fakeSessionLauncher{opts: expected})
	got, err := svc.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestSessionsService_CreateSessionValidatesAndLaunches(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	svc := newSessionsService(launcher)

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Name: "review", Prompt: "go"})
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err), "repository is required")

	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r"})
	assert.Equal(t, KindInvalid, KindOf(err), "name is required")

	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "bad~name"})
	assert.Equal(t, KindInvalid, KindOf(err), "name must be valid")

	assert.Empty(t, launcher.calls, "nothing launches until validation passes")

	out, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{
		Repository: "  https://github.com/acme/site.git  ",
		Name:       "  review-81  ",
		Prompt:     "  fix it  ",
		Agent:      " claude ",
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", out.ID)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, dispatch.LaunchSessionRequest{
		Name:   "review-81",
		Prompt: "fix it",
		Agent:  "claude",
		Repo:   "https://github.com/acme/site.git",
	}, launcher.calls[0])
}

func TestSessionsService_CreateSessionMapsDuplicateName(t *testing.T) {
	svc := newSessionsService(&fakeSessionLauncher{err: dispatch.ErrDuplicateSessionName})
	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "dupe"})
	require.Error(t, err)
	assert.Equal(t, KindConflict, KindOf(err))
}

func TestSessionsService_UnavailableWithoutLauncher(t *testing.T) {
	svc := newSessionsService(nil)
	_, err := svc.SessionLaunchOptions(t.Context())
	assert.Equal(t, KindUnavailable, KindOf(err))
	_, err = svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "n"})
	assert.Equal(t, KindUnavailable, KindOf(err))
}
