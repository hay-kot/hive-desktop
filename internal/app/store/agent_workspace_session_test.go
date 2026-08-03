package store

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

	t.Run("ListIsNewestOpenedFirstAndScopedToOneWorkspace", func(t *testing.T) {
		_, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
			Workspace: "list-scope", Name: "older", Agent: "claude", CreatedAt: 1000, LastOpenedAt: 1000,
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
}
