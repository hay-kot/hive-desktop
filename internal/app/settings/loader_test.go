package settings

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSettingsMigratesUnversionedFileAndRoundTrips(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("polling:\n  interval: 2m\n"), 0o600))

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, SettingsSchemaVersion(), cfg.Version)
	assert.Equal(t, DefaultSettings().Version, SettingsSchemaVersion())

	store := NewStore(path)
	saved, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "dark"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, SettingsSchemaVersion(), saved.Version)

	reloaded, err := store.Effective()
	require.NoError(t, err)
	assert.Equal(t, SettingsSchemaVersion(), reloaded.Version)
}

func TestLoadSettingsRejectsNewerVersionAndLeavesFileUntouched(t *testing.T) {
	path := isolateSettings(t)
	const contents = "version: 2\npolling:\n  interval: 2m\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	_, err := LoadSettings()
	require.Error(t, err)
	require.NotPanics(t, func() { _, _ = LoadSettings() })

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, contents, string(raw), "the load path is pure Apply and must never write")
}
