package settings

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	return filepath.Join(dir, settingsFileName)
}

func TestDefaultSettingsAreSafe(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, cfg.Polling.Interval.Duration())
	assert.True(t, cfg.Updates.Enabled)
	assert.True(t, cfg.Notifications.Enabled)
	assert.Equal(t, DeliveryAuto, cfg.Notifications.Delivery)
	assert.False(t, cfg.Webhooks.Enabled)
	assert.Equal(t, "127.0.0.1", cfg.Webhooks.Host)
	assert.Zero(t, cfg.Webhooks.Port)
	assert.Equal(t, MockLive, cfg.Development.Mocks.Mode)
	assert.False(t, cfg.Development.Pprof.Enabled)
}

func TestLoadSettingsStrictNestedYAMLThenEnvironment(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte(`
polling:
  interval: 2m
updates:
  enabled: false
notifications:
  enabled: true
  delivery: app
  sound: false
webhooks:
  enabled: true
  host: 127.0.0.1
  port: 24001
development:
  mocks:
    mode: pipeline
  vite:
    host: 127.0.0.1
    port: 0
  wails:
    host: 127.0.0.1
    port: 0
  pprof:
    enabled: false
    host: 127.0.0.1
    port: 0
  debug:
    pause_ingest: 0s
    pause_commit: 0s
`), 0o600))
	t.Setenv("HIVE_DESKTOP_WEBHOOKS_PORT", "25002")
	t.Setenv("HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE", "feed")
	t.Setenv("HIVE_DESKTOP_DEVELOPMENT_VITE_PORT", "43123")

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.Polling.Interval.Duration())
	assert.False(t, cfg.Updates.Enabled)
	assert.Equal(t, 25002, cfg.Webhooks.Port)
	assert.Equal(t, MockFeed, cfg.Development.Mocks.Mode)
	assert.Equal(t, 43123, cfg.Development.Vite.Port)
	assert.True(t, cfg.EnvironmentOverridden(EnvWebhookPort))

	persisted, err := LoadPersistedSettings()
	require.NoError(t, err)
	assert.Equal(t, 24001, persisted.Webhooks.Port)
	assert.Equal(t, MockPipeline, persisted.Development.Mocks.Mode)
}

func TestLoadSettingsRejectsUnknownFields(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("webhooks:\n  enabled: false\n  typo: true\n"), 0o600))
	_, err := LoadSettings()
	require.ErrorContains(t, err, "field typo not found")
}

func TestLoadSettingsRejectsInvalidEnvironment(t *testing.T) {
	isolateSettings(t)
	t.Setenv("HIVE_DESKTOP_WEBHOOKS_PORT", "not-a-port")
	_, err := LoadSettings()
	require.ErrorContains(t, err, "parse error on field \"Port\"")
}

func TestLoadSettingsRejectsUnsupportedViteHostEnvironment(t *testing.T) {
	isolateSettings(t)
	t.Setenv("HIVE_DESKTOP_DEVELOPMENT_VITE_HOST", "127.0.0.2")

	_, err := LoadSettings()
	require.ErrorContains(t, err, "development.vite.host must be 127.0.0.1")
}

func TestLoadSettingsRejectsInvalidPersistedValueShadowedByEnvironment(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("polling:\n  interval: 1s\n"), 0o600))
	t.Setenv("HIVE_DESKTOP_POLLING_INTERVAL", "5m")

	_, err := LoadSettings()
	require.ErrorContains(t, err, "validate persisted desktop settings: polling.interval")
}

func TestSaveSettingsDoesNotPersistEnvironmentOverride(t *testing.T) {
	path := isolateSettings(t)
	base := DefaultSettings()
	base.Webhooks.Port = 24001
	require.NoError(t, SaveSettings(base))
	t.Setenv(EnvWebhookPort, "25002")

	effective, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 25002, effective.Webhooks.Port)

	persisted, err := LoadPersistedSettings()
	require.NoError(t, err)
	persisted.Appearance.Theme = "dark"
	require.NoError(t, SaveSettings(persisted))

	reloaded, err := LoadPersistedSettings()
	require.NoError(t, err)
	assert.Equal(t, 24001, reloaded.Webhooks.Port)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "25002")
}

func TestSettingsValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Settings)
	}{
		{"short poll", func(s *Settings) { s.Polling.Interval = Duration(time.Second) }},
		{"update channel", func(s *Settings) { s.Updates.Channel = "nightly" }},
		{"delivery", func(s *Settings) { s.Notifications.Delivery = "desktop" }},
		{"webhook host", func(s *Settings) { s.Webhooks.Host = "0.0.0.0" }},
		{"webhook port", func(s *Settings) { s.Webhooks.Port = 80 }},
		{"mock", func(s *Settings) { s.Development.Mocks.Mode = "mystery" }},
		{"vite host", func(s *Settings) { s.Development.Vite.Host = "127.0.0.2" }},
		{"pprof host", func(s *Settings) { s.Development.Pprof.Host = "::" }},
		{"negative pause", func(s *Settings) { s.Development.Debug.PauseCommit = Duration(-time.Second) }},
		{"excessive pause", func(s *Settings) { s.Development.Debug.PauseIngest = Duration(MaxDebugPause + time.Second) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultSettings()
			tt.edit(&cfg)
			require.Error(t, cfg.Validate())
		})
	}
}

func TestStoreUpdateReappliesEnvironmentWithoutMaterializingIt(t *testing.T) {
	store := NewStore(isolateSettings(t))
	t.Setenv("HIVE_DESKTOP_NOTIFICATIONS_ENABLED", "false")

	effective, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "dark"
		cfg.Notifications.Enabled = true
		return nil
	})
	require.NoError(t, err)
	assert.False(t, effective.Notifications.Enabled)

	persisted, err := store.Persisted()
	require.NoError(t, err)
	assert.True(t, persisted.Notifications.Enabled)
	assert.Equal(t, "dark", persisted.Appearance.Theme)
}

func TestStoreSerializesConcurrentMutations(t *testing.T) {
	store := NewStore(isolateSettings(t))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := store.Update(func(cfg *Settings) error {
			cfg.Appearance.Theme = "dark"
			return nil
		})
		assert.NoError(t, err)
	}()
	go func() {
		defer wg.Done()
		_, err := store.Update(func(cfg *Settings) error {
			cfg.Webhooks.Port = 24567
			return nil
		})
		assert.NoError(t, err)
	}()
	wg.Wait()

	persisted, err := store.Persisted()
	require.NoError(t, err)
	assert.Equal(t, "dark", persisted.Appearance.Theme)
	assert.Equal(t, 24567, persisted.Webhooks.Port)
}
