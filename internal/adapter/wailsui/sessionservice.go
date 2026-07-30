package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

// SessionService is the frontend API for the New Session form and for managing
// the sessions that exist.
type SessionService struct {
	sessions *app.SessionsService
}

func NewSessionService(sessions *app.SessionsService) *SessionService {
	return &SessionService{sessions: sessions}
}

func (s *SessionService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	return s.sessions.SessionLaunchOptions(ctx)
}

// ListSessions returns every session in every state; Slug is the tmux target an
// attach uses, and only an active session has one.
func (s *SessionService) ListSessions(ctx context.Context) ([]dispatch.SessionSummary, error) {
	return s.sessions.ListSessions(ctx)
}

// SessionStatusSnapshot is one poll result in the units the browser timer uses.
type SessionStatusSnapshot struct {
	Items          []dispatch.SessionStatus `json:"items"`
	PollIntervalMS int64                    `json:"pollIntervalMs"`
}

// SessionStatuses returns the current terminal-detected agent state for each
// active session.
func (s *SessionService) SessionStatuses(ctx context.Context) (SessionStatusSnapshot, error) {
	snapshot, err := s.sessions.SessionStatuses(ctx)
	if err != nil {
		return SessionStatusSnapshot{}, err
	}
	return sessionStatusSnapshotOf(snapshot), nil
}

func sessionStatusSnapshotOf(snapshot dispatch.SessionStatusSnapshot) SessionStatusSnapshot {
	return SessionStatusSnapshot{
		Items:          snapshot.Items,
		PollIntervalMS: snapshot.PollInterval.Milliseconds(),
	}
}

// SessionDetail reads one session in full, for the detail view.
func (s *SessionService) SessionDetail(ctx context.Context, id string) (dispatch.SessionDetail, error) {
	return s.sessions.SessionDetail(ctx, id)
}

// SessionRisk reports the uncommitted or unpushed work a delete or recycle
// would discard, for the confirmation that precedes one.
func (s *SessionService) SessionRisk(ctx context.Context, id string) (dispatch.SessionRisk, error) {
	return s.sessions.SessionRisk(ctx, id)
}

// CreateSession validates the form and starts the session as a background job,
// returning the job id. Its outcome surfaces in the jobs UI.
func (s *SessionService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (int64, error) {
	return s.sessions.CreateSession(ctx, req)
}

// RenameSession renames a session and returns its new summary. The slug in it
// is the new tmux target: renaming re-slugs, so an attached caller has to
// re-attach under the name that comes back.
func (s *SessionService) RenameSession(ctx context.Context, id, name string) (dispatch.SessionSummary, error) {
	return s.sessions.RenameSession(ctx, id, name)
}

// DeleteSession starts the delete as a background job and returns the job id.
func (s *SessionService) DeleteSession(ctx context.Context, id string) (int64, error) {
	return s.sessions.DeleteSession(ctx, id)
}

// RecycleSession starts the recycle as a background job and returns the job id.
func (s *SessionService) RecycleSession(ctx context.Context, id string) (int64, error) {
	return s.sessions.RecycleSession(ctx, id)
}

// PruneSessions starts the prune of every recycled and corrupted session as a
// background job and returns the job id.
func (s *SessionService) PruneSessions(ctx context.Context) (int64, error) {
	return s.sessions.PruneSessions(ctx)
}
