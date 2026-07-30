package settings

import (
	"os"
	"path/filepath"
	"reflect"
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
	assert.True(t, cfg.HTTP.Enabled, "the loopback HTTP server is on by default")
	assert.Equal(t, "127.0.0.1", cfg.HTTP.Host)
	assert.Zero(t, cfg.HTTP.Port)
	assert.Equal(t, MockLive, cfg.Development.Mocks.Mode)
	assert.False(t, cfg.Development.Pprof.Enabled)
	assert.False(t, cfg.Experimental.Terminal, "terminal mode ships dark")
}

func TestExperimentalTerminalYAMLThenEnvironment(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("experimental:\n  terminal: true\n"), 0o600))

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.True(t, cfg.Experimental.Terminal)

	t.Setenv("HIVE_DESKTOP_EXPERIMENTAL_TERMINAL", "false")
	cfg, err = LoadSettings()
	require.NoError(t, err)
	assert.False(t, cfg.Experimental.Terminal, "the environment wins over settings.yaml")
	assert.True(t, cfg.EnvironmentOverridden("HIVE_DESKTOP_EXPERIMENTAL_TERMINAL"))
}

func TestPathsTmuxYAMLThenEnvironment(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("paths:\n  tmux: /opt/homebrew/bin/tmux\n"), 0o600))

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, "/opt/homebrew/bin/tmux", cfg.Paths.Tmux)

	t.Setenv("HIVE_DESKTOP_PATHS_TMUX", "/usr/local/bin/tmux")
	cfg, err = LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/tmux", cfg.Paths.Tmux)
	assert.True(t, cfg.EnvironmentOverridden("HIVE_DESKTOP_PATHS_TMUX"))
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
http:
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
  debug:
    pause_ingest: 0s
    pause_commit: 0s
`), 0o600))
	t.Setenv("HIVE_DESKTOP_HTTP_PORT", "25002")
	t.Setenv("HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE", "feed")
	t.Setenv("HIVE_DESKTOP_DEVELOPMENT_VITE_PORT", "43123")

	cfg, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.Polling.Interval.Duration())
	assert.False(t, cfg.Updates.Enabled)
	assert.Equal(t, 25002, cfg.HTTP.Port)
	assert.Equal(t, MockFeed, cfg.Development.Mocks.Mode)
	assert.Equal(t, 43123, cfg.Development.Vite.Port)
	assert.True(t, cfg.EnvironmentOverridden(EnvHTTPPort))

	persisted, err := LoadPersistedSettings()
	require.NoError(t, err)
	assert.Equal(t, 24001, persisted.HTTP.Port)
	assert.Equal(t, MockPipeline, persisted.Development.Mocks.Mode)
}

func TestLoadSettingsRejectsUnknownFields(t *testing.T) {
	path := isolateSettings(t)
	require.NoError(t, os.WriteFile(path, []byte("http:\n  enabled: false\n  typo: true\n"), 0o600))
	_, err := LoadSettings()
	require.ErrorContains(t, err, "field typo not found")
}

func TestLoadSettingsRejectsInvalidEnvironment(t *testing.T) {
	isolateSettings(t)
	t.Setenv("HIVE_DESKTOP_HTTP_PORT", "not-a-port")
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
	base.HTTP.Port = 24001
	require.NoError(t, SaveSettings(base))
	t.Setenv(EnvHTTPPort, "25002")

	effective, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 25002, effective.HTTP.Port)

	persisted, err := LoadPersistedSettings()
	require.NoError(t, err)
	persisted.Appearance.Theme = "dark"
	require.NoError(t, SaveSettings(persisted))

	reloaded, err := LoadPersistedSettings()
	require.NoError(t, err)
	assert.Equal(t, 24001, reloaded.HTTP.Port)
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
		{"http host", func(s *Settings) { s.HTTP.Host = "0.0.0.0" }},
		{"http port", func(s *Settings) { s.HTTP.Port = 80 }},
		{"mock", func(s *Settings) { s.Development.Mocks.Mode = "mystery" }},
		{"vite host", func(s *Settings) { s.Development.Vite.Host = "127.0.0.2" }},
		{"negative pause", func(s *Settings) { s.Development.Debug.PauseCommit = Duration(-time.Second) }},
		{"excessive pause", func(s *Settings) { s.Development.Debug.PauseIngest = Duration(MaxDebugPause + time.Second) }},
		{"github api base remote host", func(s *Settings) {
			s.Development.GitHub.APIBase = "https://api.github.example.com"
		}},
		{"github api base public ip", func(s *Settings) {
			s.Development.GitHub.APIBase = "http://10.0.0.5:8080"
		}},
		{"github api base scheme", func(s *Settings) {
			s.Development.GitHub.APIBase = "ftp://127.0.0.1:8080"
		}},
		{"github api base missing scheme", func(s *Settings) {
			s.Development.GitHub.APIBase = "127.0.0.1:8080"
		}},
		{"relative tmux path", func(s *Settings) { s.Paths.Tmux = "bin/tmux" }},
		{"bare tmux name", func(s *Settings) { s.Paths.Tmux = "tmux" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultSettings()
			tt.edit(&cfg)
			require.Error(t, cfg.Validate())
		})
	}
}

// A struct tag cannot reference a constant, so the env name is written twice.
// EnvironmentOverridden lookups key off the constant while the parser keys off
// the tag, and a silent drift between them would report "not overridden" for a
// value that was in fact overridden.
func TestEnvGitHubAPIBaseMatchesStructTag(t *testing.T) {
	field, ok := reflect.TypeFor[GitHubDevSettings]().FieldByName("APIBase")
	require.True(t, ok)
	assert.Equal(t, EnvGitHubAPIBase, field.Tag.Get("env"))
}

func TestGitHubAPIBaseAcceptsLoopbackAndNormalizes(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{"unset means api.github.com", "", ""},
		{"loopback ip", "http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"localhost", "http://localhost:8080", "http://localhost:8080"},
		{"ipv6 loopback", "http://[::1]:8080", "http://[::1]:8080"},
		{"trailing slash trimmed", "http://127.0.0.1:8080/", "http://127.0.0.1:8080"},
		{"surrounding space trimmed", "  http://127.0.0.1:8080  ", "http://127.0.0.1:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultSettings()
			cfg.Development.GitHub.APIBase = tt.set
			require.NoError(t, cfg.Validate())
			assert.Equal(t, tt.want, cfg.GitHubAPIBase())
		})
	}
}

// The override is loopback-only wherever it comes from: being a persisted
// setting must not make it a way to aim a shipped app at a remote host.
func TestGitHubAPIBaseRejectsRemoteHostFromEnvironment(t *testing.T) {
	t.Setenv(EnvGitHubAPIBase, "https://api.github.example.com")
	_, err := NewStore(isolateSettings(t)).Reload()
	require.Error(t, err)
}

func TestGitHubAPIBaseEnvironmentOverrideIsNotPersisted(t *testing.T) {
	store := NewStore(isolateSettings(t))
	t.Setenv(EnvGitHubAPIBase, "http://127.0.0.1:9999")

	effective, err := store.Update(func(cfg *Settings) error {
		cfg.Appearance.Theme = "dark"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9999", effective.GitHubAPIBase())
	assert.True(t, effective.EnvironmentOverridden(EnvGitHubAPIBase))

	persisted, err := store.Persisted()
	require.NoError(t, err)
	assert.Empty(t, persisted.GitHubAPIBase())
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
			cfg.HTTP.Port = 24567
			return nil
		})
		assert.NoError(t, err)
	}()
	wg.Wait()

	persisted, err := store.Persisted()
	require.NoError(t, err)
	assert.Equal(t, "dark", persisted.Appearance.Theme)
	assert.Equal(t, 24567, persisted.HTTP.Port)
}
