package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// Every field has to be classified as live or startup-only, or a reload
// silently does nothing for it and no restart hint ever appears. This is what
// makes adding a field to the schema a decision rather than an omission.
func TestSettingsReloadClassifiesEverySchemaField(t *testing.T) {
	schema := settings.FieldNames()
	for _, field := range schema {
		_, ok := settingsReload[field]
		assert.True(t, ok, "settings field %q is not classified in settingsReload", field)
	}
	for field := range settingsReload {
		assert.Contains(t, schema, field, "settingsReload classifies %q, which is not a settings field", field)
	}
}

func newReloadTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")

	core, err := New(t.Context(), Config{MockMode: settings.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })
	return core
}

func writeSettings(t *testing.T, core *App, contents string) {
	t.Helper()
	path := core.settingsStore.Path()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func TestReloadSettingsAdoptsLiveFieldsAndPublishes(t *testing.T) {
	core := newReloadTestApp(t)

	published := make(chan events.SettingsUpdated, 4)
	cancel := events.Subscribe(t.Context(), core.Events, "test.settings", events.Buffer(4),
		func(_ context.Context, e events.SettingsUpdated) { published <- e })
	t.Cleanup(cancel)

	writeSettings(t, core, "polling:\n  interval: 9m\npaths:\n  tmux: /nowhere/tmux\n")
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"polling.interval", "paths.tmux"}, result.Changed)
	assert.Empty(t, result.RestartPending, "both fields are adopted by the running process")

	select {
	case e := <-published:
		assert.ElementsMatch(t, []string{"polling.interval", "paths.tmux"}, e.Changed)
	case <-time.After(5 * time.Second):
		t.Fatal("no settings.updated event was published")
	}

	// The resolver forgot whatever it had memoized and is now answering for the
	// override that just arrived, rather than for the one it started with.
	_, err = core.tmux.Path()
	require.ErrorContains(t, err, "paths.tmux")
}

func TestReloadSettingsAppliesGitHubAPIBase(t *testing.T) {
	t.Setenv(settings.EnvGitHubAPIBase, "")
	core := newReloadTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"login":"reload"}`))
	}))
	t.Cleanup(server.Close)

	writeSettings(t, core, fmt.Sprintf("development:\n  github:\n    api_base: %q\n", server.URL))
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	require.Contains(t, result.Changed, "development.github.api_base")
	assert.NotContains(t, fields(result.RestartPending), "development.github.api_base")

	user, err := core.github.WithTokenCopy("token").User(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "reload", user.Login)
}

func TestReloadSettingsKeepsPhaseTwoFieldsOutOfRestartPending(t *testing.T) {
	t.Setenv(settings.EnvGitHubAPIBase, "")
	core := newReloadTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"reload"}`))
	}))
	t.Cleanup(server.Close)

	writeSettings(t, core, fmt.Sprintf("updates:\n  channel: beta\nskills:\n  auto_update: true\ndevelopment:\n  github:\n    api_base: %q\n", server.URL))
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, fields(result.RestartPending), "updates.channel")
	assert.NotContains(t, fields(result.RestartPending), "skills.auto_update")
	assert.NotContains(t, fields(result.RestartPending), "development.github.api_base")
}

func fields(pending []RestartPendingField) []string {
	result := make([]string, 0, len(pending))
	for _, field := range pending {
		result = append(result, field.Field)
	}
	return result
}

func TestReloadSettingsIsQuietWhenNothingChanged(t *testing.T) {
	core := newReloadTestApp(t)

	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	assert.Empty(t, result.Changed)
}

// A settings edit is not a reason to take a working app down: the values in
// service stay, nothing is published, and the failure is recorded so the
// degradation is not silent.
func TestReloadSettingsKeepsLastGoodOnABrokenFile(t *testing.T) {
	core := newReloadTestApp(t)
	writeSettings(t, core, "polling:\n  interval: 9m\n")
	_, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)

	writeSettings(t, core, "polling:\n  interval: \"every so often\"\n")
	_, err = core.ReloadSettings(t.Context())
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
	assert.Equal(t, 9*time.Minute, core.settingsStore.Current().Polling.Interval.Duration())

	recorded, listErr := core.Activity.List(t.Context(), 0, 20)
	require.NoError(t, listErr)
	assert.True(t, slices.ContainsFunc(recorded, func(e activity.Event) bool {
		return e.Title == "Could not reload settings.yaml"
	}), "the reload failure is in the activity log")
}

func TestSettingsStatusReportsLoadErrorAndRestartPending(t *testing.T) {
	core := newReloadTestApp(t)
	t.Setenv(settings.EnvMockMode, "")
	writeSettings(t, core, "development:\n  mocks:\n    mode: onboarding\n")
	_, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)

	status := core.SettingsStatus(t.Context())
	assert.Equal(t, core.settingsStore.Path(), status.Path)
	assert.True(t, status.Valid)
	assert.Empty(t, status.Error)
	require.Len(t, status.RestartPending, 1)
	assert.Equal(t, "development.mocks.mode", status.RestartPending[0].Field)

	writeSettings(t, core, "polling:\n  nope: true\n")
	_, err = core.ReloadSettings(t.Context())
	require.Error(t, err)

	status = core.SettingsStatus(t.Context())
	assert.False(t, status.Valid)
	assert.Contains(t, status.Error, "parse desktop settings")
	require.Len(t, status.RestartPending, 1)
	assert.Equal(t, "development.mocks.mode", status.RestartPending[0].Field)
}

func TestRestartPendingReportsOnlyStartupFields(t *testing.T) {
	core := newReloadTestApp(t)
	require.Empty(t, core.RestartPending(t.Context()))

	// experimental.terminal is mounted at composition (ADR 0037); the poll
	// interval beside it is adopted immediately and must not be reported.
	writeSettings(t, core, "polling:\n  interval: 9m\nexperimental:\n  terminal: true\n")
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"polling.interval", "experimental.terminal"}, result.Changed)

	pending := core.RestartPending(t.Context())
	require.Len(t, pending, 1)
	assert.Equal(t, "experimental.terminal", pending[0].Field)
	assert.Equal(t, "false", pending[0].Running)
	assert.Equal(t, "true", pending[0].Persisted)
	assert.NotEmpty(t, pending[0].Reason)

	// It stays pending after the change stops being new: what matters is that
	// this process is not running it, not that it changed just now.
	assert.Len(t, core.RestartPending(t.Context()), 1)
}

func TestRestartPendingExcludesHTTPFields(t *testing.T) {
	core := newReloadTestApp(t)
	writeSettings(t, core, "http:\n  enabled: false\n  host: ::1\n  port: 24601\n")
	_, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	for _, field := range fields(core.RestartPending(t.Context())) {
		assert.NotContains(t, field, "http.")
	}
}

// The headline behaviour: an edit made outside the app is picked up without
// anyone asking for a reload, and without a relaunch.
func TestReconcileHTTPSettingsMovesAndDisablesListener(t *testing.T) {
	t.Setenv(settings.EnvHTTPPort, "0")
	core := newReloadTestApp(t)
	require.NoError(t, core.Start(t.Context()))
	t.Setenv(settings.EnvHTTPPort, "")
	require.NotNil(t, core.webhook)
	require.True(t, core.webhook.Running())
	_, err := core.settingsStore.Update(func(cfg *settings.Settings) error {
		cfg.HTTP.Enabled = false
		return nil
	})
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	assert.False(t, core.webhook.Running())

	_, err = core.settingsStore.Update(func(cfg *settings.Settings) error {
		cfg.HTTP.Enabled = true
		cfg.HTTP.Port = 0
		return nil
	})
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	require.True(t, core.webhook.Running())
	assert.NotEqual(t, 0, core.webhook.Port())

	bound := core.webhook.Port()
	core.reconcileHTTPSettings()
	assert.Equal(t, bound, core.webhook.Port(), "a desired-equals-live reconcile is a no-op")
}

func TestReconcileHTTPSettingsRecoversFromBindFailure(t *testing.T) {
	t.Setenv(settings.EnvHTTPPort, "0")
	core := newReloadTestApp(t)
	require.NoError(t, core.Start(t.Context()))
	t.Setenv(settings.EnvHTTPPort, "")
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = occupied.Close() })
	addr, ok := occupied.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port

	_, err = core.settingsStore.Update(func(cfg *settings.Settings) error { cfg.HTTP.Port = port; return nil })
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	assert.False(t, core.webhook.Running())
	assert.NotEmpty(t, core.Webhooks.State(t.Context()).StartError)

	_, err = core.settingsStore.Update(func(cfg *settings.Settings) error { cfg.HTTP.Port = 0; return nil })
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	assert.True(t, core.webhook.Running())
	assert.Empty(t, core.Webhooks.State(t.Context()).StartError)
}

func TestReloadSettingsEnvOverridePinsListener(t *testing.T) {
	claimed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := claimed.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port
	require.NoError(t, claimed.Close())
	t.Setenv(settings.EnvHTTPPort, fmt.Sprint(port))
	core := newReloadTestApp(t)
	require.NoError(t, core.Start(t.Context()))
	require.True(t, core.webhook.Running())
	bound := core.webhook.Port()
	require.Equal(t, port, bound)

	writeSettings(t, core, "http:\n  port: 24601\n")
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	assert.Empty(t, result.Changed, "the environment override keeps the desired listener address unchanged")
	assert.Equal(t, bound, core.webhook.Port())
	assert.True(t, core.webhook.Running())
}

func TestReloadSettingsMockLaneWithoutListenerIsNoop(t *testing.T) {
	core := newReloadTestApp(t)
	require.Nil(t, core.webhook)

	writeSettings(t, core, "http:\n  enabled: false\n")
	result, err := core.ReloadSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"http.enabled"}, result.Changed)
	assert.NotContains(t, fields(result.RestartPending), "http.enabled")
	assert.NotPanics(t, core.reconcileHTTPSettings)
}

type orderingTerminalDrainer struct {
	endpoint string
	status   int
	err      error
}

func (d *orderingTerminalDrainer) DetachAll(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.endpoint, nil)
	if err != nil {
		d.err = err
		return nil
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		d.err = err
		return nil
	}
	defer func() { _ = response.Body.Close() }()
	d.status = response.StatusCode
	return nil
}

func TestStopListenerForRestartDetachesBeforeStoppingListener(t *testing.T) {
	t.Setenv(settings.EnvHTTPPort, "0")
	core := newReloadTestApp(t)
	require.True(t, core.MountAPI("/api/order", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	require.NoError(t, core.Start(t.Context()))
	drainer := &orderingTerminalDrainer{endpoint: fmt.Sprintf("http://127.0.0.1:%d/api/order", core.webhook.Port())}
	core.terminalDrainer = drainer

	core.stopListenerForRestart()

	require.NoError(t, drainer.err)
	assert.Equal(t, http.StatusNoContent, drainer.status, "DetachAll must run while the listener still answers")
	assert.False(t, core.webhook.Running())
}

func TestWebhookSetStateRestartsExactlyOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, settings.MockLive)
	t.Setenv(settings.EnvHTTPPort, "0")
	core, err := New(t.Context(), Config{MockMode: settings.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })
	require.NoError(t, core.Start(t.Context()))
	published := make(chan events.SettingsUpdated, 4)
	cancel := events.Subscribe(t.Context(), core.Events, "test.settings.set-state", events.Buffer(4),
		func(_ context.Context, event events.SettingsUpdated) { published <- event })
	t.Cleanup(cancel)

	require.NoError(t, core.Webhooks.SetState(t.Context(), true, "::1", 0))
	select {
	case event := <-published:
		assert.ElementsMatch(t, []string{"http.enabled", "http.host", "http.port"}, event.Changed)
	case <-time.After(5 * time.Second):
		t.Fatal("SetState did not converge the listener")
	}
	require.Eventually(t, func() bool {
		return core.webhook.Running() && core.webhook.Host() == "::1"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Never(t, func() bool {
		select {
		case <-published:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "the watcher reload after SetState must be diff-gated")
}

func TestReconcileHTTPSettingsConvergesThreeFieldUpdateWithOneRebind(t *testing.T) {
	t.Setenv(settings.EnvHTTPPort, "0")
	core := newReloadTestApp(t)
	require.NoError(t, core.Start(t.Context()))
	t.Setenv(settings.EnvHTTPPort, "")

	_, err := core.settingsStore.Update(func(cfg *settings.Settings) error {
		cfg.HTTP.Enabled = false
		return nil
	})
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	require.False(t, core.webhook.Running())

	claimed, err := net.Listen("tcp", "[::1]:0")
	require.NoError(t, err)
	addr, ok := claimed.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port
	require.NoError(t, claimed.Close())
	published := make(chan events.SettingsUpdated, 4)
	cancel := events.Subscribe(t.Context(), core.Events, "test.settings.three-fields", events.Buffer(4),
		func(_ context.Context, event events.SettingsUpdated) { published <- event })
	t.Cleanup(cancel)

	_, err = core.settingsStore.Update(func(cfg *settings.Settings) error {
		cfg.HTTP.Enabled = true
		cfg.HTTP.Host = "::1"
		cfg.HTTP.Port = port
		return nil
	})
	require.NoError(t, err)
	core.reconcileHTTPSettings()
	require.True(t, core.webhook.Running(), "%+v", core.Webhooks.State(t.Context()))
	assert.Equal(t, "::1", core.webhook.Host())
	assert.Equal(t, port, core.webhook.Port())
	core.reconcileHTTPSettings()
	core.reconcileHTTPSettings()

	select {
	case event := <-published:
		assert.ElementsMatch(t, []string{"http.enabled", "http.host", "http.port"}, event.Changed)
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not rebind")
	}
	assert.Never(t, func() bool {
		select {
		case <-published:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "subsequent reconciles must be no-ops once desired equals live")
}

func TestSettingsWatcherAdoptsAnEditMadeOutsideTheApp(t *testing.T) {
	core := newReloadTestApp(t)
	require.NotNil(t, core.settingsWatcher, "the watcher is what makes a hand edit live")

	published := make(chan events.SettingsUpdated, 4)
	cancel := events.Subscribe(t.Context(), core.Events, "test.settings.watch", events.Buffer(4),
		func(_ context.Context, e events.SettingsUpdated) { published <- e })
	t.Cleanup(cancel)
	require.NoError(t, core.Start(t.Context()))

	writeSettings(t, core, "polling:\n  interval: 9m\n")

	select {
	case e := <-published:
		assert.Contains(t, e.Changed, "polling.interval")
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher did not reload settings.yaml")
	}
	assert.Equal(t, 9*time.Minute, core.settingsStore.Current().Polling.Interval.Duration())
}
