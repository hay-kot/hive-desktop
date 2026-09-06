package stores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// AgentSessionStore owns agent_workspace_session: the launch table an agent
// workspace terminal is resolved from and reattached through.
type AgentSessionStore struct {
	q      *queries.DB
	now    func() time.Time
	mapper MapFunc[queries.AgentWorkspaceSession, AgentSession]
}

func NewAgentSessionStore(q *queries.DB, opts Options) *AgentSessionStore {
	return &AgentSessionStore{q: q, now: opts.Now, mapper: mapAgentSessionFromDB}
}

// List returns one workspace's sessions, newest record first -- creation
// order, so a resume never reorders the list.
func (s *AgentSessionStore) List(ctx context.Context, workspace string) ([]AgentSession, error) {
	rows, err := s.q.Ctx(ctx).ListAgentWorkspaceSessions(ctx, workspace)
	return s.mapper.SliceErr(rows, wrap("listing agent workspace sessions", err))
}

// ListAll returns every session across every workspace, newest record
// first: the same stable creation order List uses.
func (s *AgentSessionStore) ListAll(ctx context.Context) ([]AgentSession, error) {
	rows, err := s.q.Ctx(ctx).ListAllAgentWorkspaceSessions(ctx)
	return s.mapper.SliceErr(rows, wrap("listing all agent workspace sessions", err))
}

// Get reads one session by id.
func (s *AgentSessionStore) Get(ctx context.Context, id int64) (AgentSession, bool, error) {
	row, err := s.q.Ctx(ctx).GetAgentWorkspaceSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentSession{}, false, nil
	}
	if err != nil {
		return AgentSession{}, false, wrap("getting agent workspace session", err)
	}
	return s.mapper(row), true, nil
}

// Create persists one session and returns the stored row with its assigned
// id, stamping CreatedAt and LastOpenedAt with the store's own clock.
func (s *AgentSessionStore) Create(ctx context.Context, in AgentSessionCreate) (AgentSession, error) {
	now := s.now().UnixMilli()
	row, err := s.q.Ctx(ctx).InsertAgentWorkspaceSession(ctx, queries.InsertAgentWorkspaceSessionParams{
		Workspace: in.Workspace, Name: in.Name, Agent: in.Agent, AgentSessionID: in.AgentSessionID,
		CreatedAt: now, LastOpenedAt: now,
	})
	return s.mapper.Err(row, wrap("creating agent workspace session", err))
}

// Touch advances a session's last_opened_at, the signal its ordering reads.
func (s *AgentSessionStore) Touch(ctx context.Context, id, at int64) error {
	return wrap("touching agent workspace session", s.q.Ctx(ctx).TouchAgentWorkspaceSession(ctx, queries.TouchAgentWorkspaceSessionParams{
		LastOpenedAt: at,
		ID:           id,
	}))
}

// SetAgentID records the id a resume-that-fell-back-to-fresh now runs
// under, so a later resume of this same record addresses the conversation
// actually running rather than the one it replaced.
func (s *AgentSessionStore) SetAgentID(ctx context.Context, id int64, agentSessionID string) error {
	return wrap("setting agent workspace session agent id", s.q.Ctx(ctx).SetAgentWorkspaceSessionAgentID(ctx, queries.SetAgentWorkspaceSessionAgentIDParams{
		AgentSessionID: agentSessionID,
		ID:             id,
	}))
}

// Rename sets a session's display name. Presentation only: the tmux session
// name derives from the id, so a rename never touches a live terminal.
func (s *AgentSessionStore) Rename(ctx context.Context, id int64, name string) error {
	return wrap("renaming agent workspace session", s.q.Ctx(ctx).RenameAgentWorkspaceSession(ctx, queries.RenameAgentWorkspaceSessionParams{
		Name: name,
		ID:   id,
	}))
}

// Delete removes one session record. A record deleted around a live
// terminal orphans a running agent, so the caller closes the terminal
// first (AgentWorkspacesService.DeleteSession).
func (s *AgentSessionStore) Delete(ctx context.Context, id int64) error {
	return wrap("deleting agent workspace session", s.q.Ctx(ctx).DeleteAgentWorkspaceSession(ctx, id))
}

// DeleteByWorkspace removes every session record for a workspace. Removing
// the directory those records name is the service's job
// (AgentWorkspacesService.DeleteWorkspace); this is the history alone.
func (s *AgentSessionStore) DeleteByWorkspace(ctx context.Context, workspace string) error {
	return wrap("deleting agent workspace sessions by workspace", s.q.Ctx(ctx).DeleteAgentWorkspaceSessionsByWorkspace(ctx, workspace))
}
