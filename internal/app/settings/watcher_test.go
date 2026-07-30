package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func waitForSettingsChange(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not fire")
	}
}

func TestWatcherFiresOnWriteAndAtomicReplace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, settingsFileName)
	changed := make(chan struct{}, 8)

	watcher, err := NewWatcher(path, func() { changed <- struct{}{} }, zerolog.Nop())
	require.NoError(t, err)
	watcher.Start()
	t.Cleanup(watcher.Close)

	require.NoError(t, os.WriteFile(path, []byte("polling:\n  interval: 2m\n"), 0o600))
	waitForSettingsChange(t, changed)

	// The rename path: how an editor saves, and how Store.Update writes.
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte("polling:\n  interval: 3m\n"), 0o600))
	require.NoError(t, os.Rename(tmp, path))
	waitForSettingsChange(t, changed)
}

// Store.Update writes through a `.settings-*.yaml` sibling in the same
// directory. A prefix match on the basename would reload on every one of them.
func TestWatcherIgnoresTheAtomicWriteSiblings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	changed := make(chan struct{}, 8)

	watcher, err := NewWatcher(filepath.Join(dir, settingsFileName), func() { changed <- struct{}{} }, zerolog.Nop())
	require.NoError(t, err)
	watcher.Start()
	t.Cleanup(watcher.Close)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".settings-12345.yaml"), []byte("x: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.yml"), []byte("version: 1\n"), 0o600))
	select {
	case <-changed:
		t.Fatal("watcher fired for a file that is not settings.yaml")
	case <-time.After(2 * watchDebounce):
	}
}

// A config directory that does not exist yet is the first-run case: the watch
// has to be establishable before anything writes settings.yaml.
func TestWatcherCreatesTheConfigDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "config")
	changed := make(chan struct{}, 8)

	watcher, err := NewWatcher(filepath.Join(dir, settingsFileName), func() { changed <- struct{}{} }, zerolog.Nop())
	require.NoError(t, err)
	watcher.Start()
	t.Cleanup(watcher.Close)

	require.NoError(t, os.WriteFile(filepath.Join(dir, settingsFileName), []byte("polling:\n  interval: 2m\n"), 0o600))
	waitForSettingsChange(t, changed)
}

func TestWatcherCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	watcher, err := NewWatcher(filepath.Join(t.TempDir(), settingsFileName), func() {}, zerolog.Nop())
	require.NoError(t, err)
	watcher.Start()
	watcher.Close()
	require.NotPanics(t, watcher.Close)
}
