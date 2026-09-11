package settings

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

func TestLoadSettingsMigratesUnversionedFileAndRoundTrips(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("polling:\n  interval: 2m\n"), 0o600))

	current := configmigrate.SettingsSet.Current

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, current, cfg.Version)
	assert.Equal(t, current, DefaultSettings().Version)

	store := NewStore(path)
	saved, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "dark"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, current, saved.Version)

	reloaded, err := store.Effective()
	require.NoError(t, err)
	assert.Equal(t, current, reloaded.Version)
}

func TestStoreUpdateRefusesMalformedDiskWithoutOverwritingIt(t *testing.T) {
	path := isolateSettings(t)
	malformed := []byte("version: [\n")
	require.NoError(t, os.WriteFile(path, malformed, 0o600))

	_, err := NewStore(path).Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "dark"
		return nil
	})
	require.Error(t, err)
	actual, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, malformed, actual, "an unrelated settings mutation must not replace malformed disk content")
}

func TestLoadSettingsRuntimeMigrationLeavesDiskBytesUntouched(t *testing.T) {
	path := isolateSettings(t)
	old := []byte("version: 1\nexperimental:\n  terminal: true\nskills:\n  auto_update: true\n")
	require.NoError(t, os.WriteFile(path, old, 0o600))

	cfg, err := NewStore(path).Effective()
	require.NoError(t, err)
	assert.Equal(t, configmigrate.SettingsSet.Current, cfg.Version)
	actual, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, old, actual, "runtime migration must not rewrite the observed file")
}

func TestLoadSettingsRejectsNewerVersionAndLeavesFileUntouched(t *testing.T) {
	path := isolateSettings(t)
	contents := fmt.Sprintf("version: %d\npolling:\n  interval: 2m\n", configmigrate.SettingsSet.Current+1)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	_, err := LoadSettings()
	require.Error(t, err)
	require.NotPanics(t, func() { _, _ = LoadSettings() })

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, contents, string(raw), "the load path is pure Apply and must never write")
}
