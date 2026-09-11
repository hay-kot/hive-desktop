package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func TestMigrateBeforeLoadMigratesOldFixtureBeforeRegistration(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	workspaceDir := filepath.Join(workspaceRoot, "demo")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o700))
	manifestPath := filepath.Join(workspaceDir, "agent-workspace.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("version: 4\nname: Demo\nagent: claude\ncommand: claude\n"), 0o600))

	paths := settings.Paths{
		FlowsDir:           filepath.Join(root, "flows"),
		ActionsPath:        filepath.Join(root, "actions.yml"),
		AgentWorkspacesDir: workspaceRoot,
	}
	logger := zerolog.Nop()
	registered := false
	err := migrateBeforeLoad(func() {
		migrateConfiguration(paths, filepath.Join(root, "backups"), &logger)
	}, func() error {
		raw, readErr := os.ReadFile(manifestPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(raw), "version: 5", "registration must observe migrated bytes")
		assert.NotContains(t, string(raw), "agent:")

		store := agentws.NewStore(workspaceRoot)
		require.NoError(t, store.Reload())
		status, ok := store.Status("demo")
		require.True(t, ok)
		assert.True(t, status.Valid)
		registered = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, registered, "load/registration follows migration")
}
