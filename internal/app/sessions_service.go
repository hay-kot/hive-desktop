package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

// newSessionJobActionID labels form-created session jobs in the jobs UI.
const newSessionJobActionID = "new-session"

type sessionLauncher interface {
	LaunchSession(context.Context, dispatch.LaunchSessionRequest) (dispatch.SessionExecutionOutcome, error)
	SessionLaunchOptions(context.Context) (dispatch.SessionLaunchOptions, error)
}

// sessionJobRunner runs the session launch as a tracked background job so a
// slow clone does not block the caller.
type sessionJobRunner interface {
	Track(ctx context.Context, label, actionID, target string, fn func(context.Context) error) int64
}

// SessionsService creates Hive sessions from a user-driven form, through the
// same dispatch path a flow's launch-session action uses.
type SessionsService struct {
	launcher sessionLauncher
	jobs     sessionJobRunner
}

func newSessionsService(launcher sessionLauncher, jobs sessionJobRunner) *SessionsService {
	return &SessionsService{launcher: launcher, jobs: jobs}
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

// CreateSession validates a New Session form, then launches the session as a
// background job and returns its id. Creation (which may clone a repository)
// runs asynchronously and its outcome surfaces in the jobs UI, so only
// validation errors are returned here.
func (s *SessionsService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (int64, error) {
	if s.launcher == nil || s.jobs == nil {
		return 0, Errorf(KindUnavailable, "session creation is unavailable")
	}

	repo := strings.TrimSpace(req.Repository)
	if repo == "" {
		return 0, Errorf(KindInvalid, "repository is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return 0, Errorf(KindInvalid, "session name is required")
	}
	if err := dispatch.ValidateSessionName(name); err != nil {
		return 0, Wrap(err, KindInvalid, "session name")
	}

	launch := dispatch.LaunchSessionRequest{
		Name:   name,
		Prompt: strings.TrimSpace(req.Prompt),
		Agent:  strings.TrimSpace(req.Agent),
		Repo:   repo,
	}
	jobID := s.jobs.Track(ctx, "Create session", newSessionJobActionID, name, func(bg context.Context) error {
		if _, err := s.launcher.LaunchSession(bg, launch); err != nil {
			if errors.Is(err, dispatch.ErrDuplicateSessionName) {
				return fmt.Errorf("a session named %q already exists", name)
			}
			return err
		}
		return nil
	})
	return jobID, nil
}
