package queries

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentWorkspaceSession(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	t.Run("CreateGetRoundTrip", func(t *testing.T) {
		created, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "demo", Name: "first pass", Agent: "claude",
			AgentSessionID: "sess-1", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		assert.NotZero(t, created.ID)
		assert.Equal(t, "demo", created.Workspace)
		assert.Equal(t, "first pass", created.Name)
		assert.Equal(t, "claude", created.Agent)
		assert.Equal(t, "sess-1", created.AgentSessionID)

		got, ok, err := db.GetAgentWorkspaceSession(ctx, created.ID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, created, got)
	})

	t.Run("GetMissingIsNotFoundNotError", func(t *testing.T) {
		_, ok, err := db.GetAgentWorkspaceSession(ctx, 999999)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Touch", func(t *testing.T) {
		created, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "demo", Name: "touch me", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)

		require.NoError(t, db.TouchAgentWorkspaceSession(ctx, created.ID, 2000))
		got, ok, err := db.GetAgentWorkspaceSession(ctx, created.ID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, int64(2000), got.LastOpenedAt)
		assert.Equal(t, int64(1000), got.CreatedAt, "touch never changes created_at")
	})

	t.Run("SetAgentID", func(t *testing.T) {
		created, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "demo", Name: "rotate id", Agent: "codex", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		assert.Empty(t, created.AgentSessionID, "the default is empty until a launch sets it")

		require.NoError(t, db.SetAgentWorkspaceSessionAgentID(ctx, created.ID, "minted-later"))
		got, ok, err := db.GetAgentWorkspaceSession(ctx, created.ID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "minted-later", got.AgentSessionID)
	})

	t.Run("Delete", func(t *testing.T) {
		created, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "demo", Name: "delete me", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)

		require.NoError(t, db.DeleteAgentWorkspaceSession(ctx, created.ID))
		_, ok, err := db.GetAgentWorkspaceSession(ctx, created.ID)
		require.NoError(t, err)
		assert.False(t, ok)

		// Deleting an id with no session is a no-op, not an error, matching
		// every other delete in this package (DeleteNodeKV, DeleteSourceHead).
		require.NoError(t, db.DeleteAgentWorkspaceSession(ctx, created.ID))
	})

	t.Run("ListIsNewestRecordFirstAndScopedToOneWorkspace", func(t *testing.T) {
		// The first record carries the later last_opened_at on purpose: the
		// order is creation order, so touching a session (resuming it) must
		// not move its row.
		_, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "list-scope", Name: "older", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 9000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "list-scope", Name: "newer", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 2000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "other-workspace", Name: "elsewhere", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 3000,
		})
		require.NoError(t, err)

		sessions, err := db.ListAgentWorkspaceSessions(ctx, "list-scope")
		require.NoError(t, err)
		require.Len(t, sessions, 2)
		assert.Equal(t, "newer", sessions[0].Name)
		assert.Equal(t, "older", sessions[1].Name)
	})

	t.Run("DeleteByWorkspaceLeavesOtherWorkspacesAlone", func(t *testing.T) {
		_, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "delete-scope-a", Name: "a1", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "delete-scope-a", Name: "a2", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "delete-scope-b", Name: "b1", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)

		require.NoError(t, db.DeleteAgentWorkspaceSessionsByWorkspace(ctx, "delete-scope-a"))

		sessionsA, err := db.ListAgentWorkspaceSessions(ctx, "delete-scope-a")
		require.NoError(t, err)
		assert.Empty(t, sessionsA)

		sessionsB, err := db.ListAgentWorkspaceSessions(ctx, "delete-scope-b")
		require.NoError(t, err)
		assert.Len(t, sessionsB, 1)
	})

	t.Run("TwoSessionsMayShareAName", func(t *testing.T) {
		first, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "shared-name-scope", Name: "duplicate", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		second, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "shared-name-scope", Name: "duplicate", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		assert.NotEqual(t, first.ID, second.ID, "the row id is the identity, not the name")

		sessions, err := db.ListAgentWorkspaceSessions(ctx, "shared-name-scope")
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})

	t.Run("ListAllOrdersAcrossWorkspacesByNewestRecord", func(t *testing.T) {
		// last_opened_at values run counter to insertion order on purpose:
		// the cross-workspace list keeps creation order too, so a resume
		// never reshuffles it.
		_, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "all-scope-a", Name: "first", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 3000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "all-scope-b", Name: "second", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)
		_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "all-scope-a", Name: "third", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 2000,
		})
		require.NoError(t, err)

		all, err := db.ListAllAgentWorkspaceSessions(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(all), 3, "carries every prior t.Run's rows too, since ListAll is unscoped")

		var names []string
		for _, s := range all {
			if s.Workspace == "all-scope-a" || s.Workspace == "all-scope-b" {
				names = append(names, s.Name)
			}
		}
		assert.Equal(t, []string{"third", "second", "first"}, names, "newest record first regardless of workspace or last_opened_at")
	})

	t.Run("Rename", func(t *testing.T) {
		created, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "demo", Name: "New Chat", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
		})
		require.NoError(t, err)

		require.NoError(t, db.RenameAgentWorkspaceSession(ctx, created.ID, "triage the flaky test"))
		got, ok, err := db.GetAgentWorkspaceSession(ctx, created.ID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "triage the flaky test", got.Name)
		assert.Equal(t, created.AgentSessionID, got.AgentSessionID, "a rename touches only the name")
	})
}
