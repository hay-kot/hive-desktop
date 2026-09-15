package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func TestConfigurationCharacterizationInitializesAfterSettingsMigration(t *testing.T) {
	unsetDefaultAgentForAppTest(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv(settings.EnvHiveDataDir, filepath.Join(root, "hive"))
	t.Setenv(settings.EnvConfigDir, configDir)
	t.Setenv(settings.EnvMockMode, settings.MockFeed)
	require.NoError(t, os.MkdirAll(configDir, 0o700))

	settingsPath := filepath.Join(configDir, "settings.yaml")
	require.NoError(t, os.WriteFile(settingsPath, []byte("version: 1\nexperimental:\n  terminal: true\ndevelopment:\n  mocks:\n    mode: feed\n"), 0o600))
	logger := zerolog.Nop()
	_, changed, err := configmigrate.MigrateFile(configmigrate.SettingsSet, settingsPath, filepath.Join(root, "backups"), &logger)
	require.NoError(t, err)
	require.True(t, changed)

	store := settings.NewStore(settingsPath)
	cfg, err := store.Effective()
	require.NoError(t, err)
	assert.Equal(t, configmigrate.SettingsSet.Current, cfg.Version)
	assert.Equal(t, settings.MockFeed, cfg.MockMode())

	paths := settings.ResolvePaths(settings.Bootstrap{}, settings.ResolveOptions{MockMode: cfg.MockMode()})
	core, err := New(t.Context(), Config{
		Settings:      cfg,
		SettingsStore: store,
		Paths:         paths,
		MockMode:      cfg.MockMode(),
		Logger:        logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	require.NotNil(t, core.actionStore)
	require.NotNil(t, core.flowStore)
	require.NotNil(t, core.agentWorkspaceStore)
	require.NoError(t, core.Start(t.Context()))
}
