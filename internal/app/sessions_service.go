package app

import (
	"context"
	"errors"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

type sessionLauncher interface {
	LaunchSession(context.Context, dispatch.LaunchSessionRequest) (dispatch.SessionExecutionOutcome, error)
	SessionLaunchOptions(context.Context) (dispatch.SessionLaunchOptions, error)
}

// SessionsService creates Hive sessions from a user-driven form, through the
// same dispatch path a flow's launch-session action uses.
type SessionsService struct {
	launcher sessionLauncher
}

func newSessionsService(launcher sessionLauncher) *SessionsService {
	return &SessionsService{launcher: launcher}
}

// SessionLaunchOptions supplies the configured repository and agent choices the
// New Session form presents.
func (s *SessionsService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	if s.launcher == nil {
		return dispatch.SessionLaunchOptions{}, Errorf(KindUnavailable, "session launch options are unavailable")
	}
	opts, err := s.launcher.SessionLaunchOptions(ctx)
	return opts, Wrap(err, KindInternal, "resolving session launch options")
}

// CreateSession validates a New Session form and launches the session.
func (s *SessionsService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (dispatch.SessionExecutionOutcome, error) {
	if s.launcher == nil {
		return dispatch.SessionExecutionOutcome{}, Errorf(KindUnavailable, "session creation is unavailable")
	}

	repo := strings.TrimSpace(req.Repository)
	if repo == "" {
		return dispatch.SessionExecutionOutcome{}, Errorf(KindInvalid, "repository is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return dispatch.SessionExecutionOutcome{}, Errorf(KindInvalid, "session name is required")
	}
	if err := dispatch.ValidateSessionName(name); err != nil {
		return dispatch.SessionExecutionOutcome{}, Wrap(err, KindInvalid, "session name")
	}

	outcome, err := s.launcher.LaunchSession(ctx, dispatch.LaunchSessionRequest{
		Name:   name,
		Prompt: strings.TrimSpace(req.Prompt),
		Agent:  strings.TrimSpace(req.Agent),
		Repo:   repo,
	})
	if err != nil {
		if errors.Is(err, dispatch.ErrDuplicateSessionName) {
			return dispatch.SessionExecutionOutcome{}, Wrap(err, KindConflict, "a session named %q already exists", name)
		}
		return dispatch.SessionExecutionOutcome{}, Wrap(err, KindInternal, "creating session %q", name)
	}
	return outcome, nil
}
