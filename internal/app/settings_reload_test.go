package app

import (
	"context"
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
	for _, field := range settings.FieldNames() {
		_, ok := settingsReload[field]
		assert.True(t, ok, "settings field %q is not classified in settingsReload", field)
	}
	for field := range settingsReload {
		_, ok := settings.FieldValue(settings.DefaultSettings(), field)
		assert.True(t, ok, "settingsReload classifies %q, which is not a settings field", field)
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

// The port the listener actually bound is what this process is running, so the
// write-back of an automatic port must not read as a pending restart.
func TestRestartPendingIgnoresTheAllocatedPortWriteBack(t *testing.T) {
	core := newReloadTestApp(t)
	core.setMountedHTTPPort(24601)

	_, err := core.settingsStore.Update(func(cfg *settings.Settings) error {
		cfg.HTTP.Port = 24601
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, core.RestartPending(t.Context()))
}

// The headline behaviour: an edit made outside the app is picked up without
// anyone asking for a reload, and without a relaunch.
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
