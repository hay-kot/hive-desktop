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

func (s *SessionService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (dispatch.SessionExecutionOutcome, error) {
	return s.sessions.CreateSession(ctx, req)
}
