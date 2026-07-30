package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

// SessionService is the frontend API for the New Session form.
type SessionService struct {
	sessions *app.SessionsService
}

func NewSessionService(sessions *app.SessionsService) *SessionService {
	return &SessionService{sessions: sessions}
}

func (s *SessionService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	return s.sessions.SessionLaunchOptions(ctx)
}

// ListSessions returns the active sessions the terminal picker offers; Slug is
// the tmux target an attach uses.
func (s *SessionService) ListSessions(ctx context.Context) ([]dispatch.SessionSummary, error) {
	return s.sessions.ListSessions(ctx)
}

// CreateSession validates the form and starts the session as a background job,
// returning the job id. Its outcome surfaces in the jobs UI.
func (s *SessionService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (int64, error) {
	return s.sessions.CreateSession(ctx, req)
}
