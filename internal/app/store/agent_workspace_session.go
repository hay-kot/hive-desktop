package store

import (
	"context"
	"database/sql"
	"errors"
)

// ListAgentWorkspaceSessions returns one workspace's sessions, most recently
// opened first.
func (db *DB) ListAgentWorkspaceSessions(ctx context.Context, workspace string) ([]AgentWorkspaceSession, error) {
	rows, err := db.queries.ListAgentWorkspaceSessions(ctx, workspace)
	return rows, wrap("listing agent workspace sessions", err)
}

// GetAgentWorkspaceSession reads one session by id. ok reports whether it
// exists.
func (db *DB) GetAgentWorkspaceSession(ctx context.Context, id int64) (AgentWorkspaceSession, bool, error) {
	row, err := db.queries.GetAgentWorkspaceSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentWorkspaceSession{}, false, nil
	}
	if err != nil {
		return AgentWorkspaceSession{}, false, wrap("getting agent workspace session", err)
	}
	return row, true, nil
}

// CreateAgentWorkspaceSession persists one session and returns the stored row
// with its assigned id.
func (db *DB) CreateAgentWorkspaceSession(ctx context.Context, s AgentWorkspaceSession) (AgentWorkspaceSession, error) {
	row, err := db.queries.InsertAgentWorkspaceSession(ctx, InsertAgentWorkspaceSessionParams{
		Workspace:      s.Workspace,
		Name:           s.Name,
		Agent:          s.Agent,
		AgentSessionID: s.AgentSessionID,
		CreatedAt:      s.CreatedAt,
		LastOpenedAt:   s.LastOpenedAt,
	})
	return row, wrap("creating agent workspace session", err)
}

// TouchAgentWorkspaceSession advances a session's last_opened_at, the signal
// its ordering reads.
func (db *DB) TouchAgentWorkspaceSession(ctx context.Context, id, at int64) error {
	return wrap("touching agent workspace session", db.queries.TouchAgentWorkspaceSession(ctx, TouchAgentWorkspaceSessionParams{
		LastOpenedAt: at,
		ID:           id,
	}))
}

// SetAgentWorkspaceSessionAgentID records the id a resume-that-fell-back-to-
// fresh now runs under, so a later resume of this same record addresses the
// conversation actually running rather than the one it replaced.
func (db *DB) SetAgentWorkspaceSessionAgentID(ctx context.Context, id int64, agentSessionID string) error {
	return wrap("setting agent workspace session agent id", db.queries.SetAgentWorkspaceSessionAgentID(ctx, SetAgentWorkspaceSessionAgentIDParams{
		AgentSessionID: agentSessionID,
		ID:             id,
	}))
}

// DeleteAgentWorkspaceSession removes one session record. A record deleted
// around a live terminal orphans a running agent, so the caller closes the
// terminal first (AgentWorkspacesService.DeleteSession).
func (db *DB) DeleteAgentWorkspaceSession(ctx context.Context, id int64) error {
	return wrap("deleting agent workspace session", db.queries.DeleteAgentWorkspaceSession(ctx, id))
}

// DeleteAgentWorkspaceSessionsByWorkspace removes every session record for a
// workspace. It never touches the workspace directory itself: deleting a
// workspace deletes its session history, not the user's files.
func (db *DB) DeleteAgentWorkspaceSessionsByWorkspace(ctx context.Context, workspace string) error {
	return wrap("deleting agent workspace sessions by workspace", db.queries.DeleteAgentWorkspaceSessionsByWorkspace(ctx, workspace))
}
