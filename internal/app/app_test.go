package app

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// TestAppLifecycle is the cheapest proof that the wiring package main used to
// own survived the move intact: the core builds, starts, and unwinds.
//
// The goroutine assertion is the part that matters. Every background
// subsystem here — the producer, the output worker, retention, the config
// watchers, the event bus and its subscribers, the vendored hive event bus —
// detaches a goroutine, and a Close that forgets one leaks it silently for
// the life of the process. Nothing else in the suite would notice.
//
// go.uber.org/goleak would say this more precisely; it is not in go.mod and
// one assertion does not justify a dependency.
func TestAppLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HIVE_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
	// Mock mode has no live producer and no keychain access, which is what
	// makes this runnable anywhere.
	t.Setenv(settings.EnvMockMode, "feed")

	settle(t)
	before := runtime.NumGoroutine()

	core, err := New(t.Context(), Config{
		Settings: settings.Settings{},
		MockMode: settings.MockMode(),
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)

	// The facade is whole: every service an adapter mounts is non-nil.
	require.NotNil(t, core.Inbox)
	require.NotNil(t, core.Flows)
	require.NotNil(t, core.Actions)
	require.NotNil(t, core.Settings)
	require.NotNil(t, core.System)
	require.NotNil(t, core.Webhooks)
	require.NotNil(t, core.Auth)
	require.NotNil(t, core.Activity)
	require.NotNil(t, core.Jobs)
	require.NotNil(t, core.Prompts)
	require.NotNil(t, core.Events)
	require.NotNil(t, core.Store)

	require.NoError(t, core.Start(t.Context()))
	require.NoError(t, core.Close())

	// Close is idempotent: a failed Run calls shutdown and so does a clean
	// one, and on some paths both.
	require.NotPanics(t, func() { _ = core.Close() })

	// A plain loop rather than assert.Eventually: that helper evaluates its
	// condition on a goroutine of its own, which the count would include, so
	// the assertion could never pass.
	after := runtime.NumGoroutine()
	for range 500 {
		if after <= before {
			break
		}
		time.Sleep(20 * time.Millisecond)
		after = runtime.NumGoroutine()
	}
	if after > before {
		buf := make([]byte, 1<<20)
		t.Log(string(buf[:runtime.Stack(buf, true)]))
	}
	assert.LessOrEqual(t, after, before, "Close leaked a goroutine")
}

// settle waits for goroutines left over from earlier tests in this package to
// exit, so the baseline is this test's own.
func settle(t *testing.T) {
	t.Helper()
	baseline := runtime.NumGoroutine()
	for range 50 {
		time.Sleep(10 * time.Millisecond)
		current := runtime.NumGoroutine()
		if current >= baseline {
			return
		}
		baseline = current
	}
}
