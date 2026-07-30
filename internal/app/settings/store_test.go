package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSettings(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func TestStoreCurrentDefaultsWhenNothingIsPersisted(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "settings.yaml"))

	assert.Equal(t, DefaultSettings().Polling.Interval, store.Current().Polling.Interval)
	assert.NoError(t, store.LoadError(), "a missing file is the safe default, not a failure")
}

func TestStoreReloadSwapsTheSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, "polling:\n  interval: 2m\n")
	store := NewStore(path)
	require.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())

	writeSettings(t, path, "polling:\n  interval: 9m\n")
	assert.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration(),
		"the snapshot only moves on reload; nothing re-reads the file behind the caller")

	reloaded, err := store.Reload()
	require.NoError(t, err)
	assert.Equal(t, 9*time.Minute, reloaded.Polling.Interval.Duration())
	assert.Equal(t, 9*time.Minute, store.Current().Polling.Interval.Duration())
}

// The whole point of the snapshot: an edit that will not parse must not take
// the running app's settings away from it.
func TestStoreKeepsLastGoodThroughABrokenEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, "polling:\n  interval: 2m\n")
	store := NewStore(path)
	require.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())

	writeSettings(t, path, "polling:\n  interval: \"every so often\"\n")
	served, err := store.Reload()
	require.Error(t, err)
	assert.Equal(t, 2*time.Minute, served.Polling.Interval.Duration(), "reload returns what stays in service")
	assert.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())
	require.Error(t, store.LoadError(), "the snapshot is stale and the store says so")

	writeSettings(t, path, "polling:\n  interval: 3m\n")
	repaired, err := store.Reload()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, repaired.Polling.Interval.Duration())
	assert.NoError(t, store.LoadError())
}

// A value that fails validation is as broken as one that fails to parse: the
// persisted file has to stand on its own (ADR 0014).
func TestStoreReloadRejectsAnInvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, "polling:\n  interval: 2m\n")
	store := NewStore(path)
	require.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())

	writeSettings(t, path, "polling:\n  interval: 1s\n")
	_, err := store.Reload()
	require.Error(t, err)
	assert.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())
}

func TestStoreUpdateRefreshesTheSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	store := NewStore(path)

	saved, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "gruvbox"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "gruvbox", saved.Appearance.Theme)
	assert.Equal(t, "gruvbox", store.Current().Appearance.Theme, "a write is not a reason to go back to disk")
}

// A write must not fall back to the snapshot: last-good keeps the app reading,
// but writing it back would overwrite whatever is mid-edit on disk.
func TestStoreUpdateFailsWhileTheFileIsBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, "polling:\n  interval: 2m\n")
	store := NewStore(path)
	require.Equal(t, 2*time.Minute, store.Current().Polling.Interval.Duration())

	const broken = "polling:\n  interval: \"every so often\"\n"
	writeSettings(t, path, broken)
	_, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "nord"
		return nil
	})
	require.Error(t, err)

	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, broken, string(raw), "the user's file is left exactly as they left it")
}

// The environment override is process-local and must reach the snapshot without
// ever being written into YAML (ADR 0014).
func TestStoreCurrentAppliesEnvironmentOverridesWithoutPersistingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeSettings(t, path, "polling:\n  interval: 2m\n")
	t.Setenv("HIVE_DESKTOP_POLLING_INTERVAL", "7m")
	store := NewStore(path)

	assert.Equal(t, 7*time.Minute, store.Current().Polling.Interval.Duration())

	_, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "nord"
		return nil
	})
	require.NoError(t, err)

	persisted, err := store.Persisted()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, persisted.Polling.Interval.Duration(),
		"an unrelated write must not materialize the override into settings.yaml")
	assert.Equal(t, 7*time.Minute, store.Current().Polling.Interval.Duration())
}
