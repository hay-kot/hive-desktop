package wailsui

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// fakeEngine is a test double for the Updater. It records call counts and
// returns a scripted release / error.
type fakeEngine struct {
	mu         sync.Mutex
	checks     int
	installs   int
	restarts   int
	rel        *updater.Release
	checkErr   error
	installErr error
	restartErr error
}

func (f *fakeEngine) Check(context.Context) (*updater.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.rel, f.checkErr
}

func (f *fakeEngine) DownloadAndInstall(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	return f.installErr
}

func (f *fakeEngine) Restart(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	return f.restartErr
}

func (f *fakeEngine) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

// silenceEmits swaps the package-level emit hooks for no-ops so tests don't
// depend on a running application.
func silenceEmits(t *testing.T) {
	t.Helper()
	origA, origN := emitUpdateAvailable, emitUpdateNone
	emitUpdateAvailable = func(UpdateInfo) {}
	emitUpdateNone = func(UpdateInfo) {}
	t.Cleanup(func() { emitUpdateAvailable, emitUpdateNone = origA, origN })
}

func TestUpdaterServiceStatusDefault(t *testing.T) {
	silenceEmits(t)
	s := NewUpdaterService("1.2.3", true, time.Hour, zerolog.Nop())
	got := s.Status()
	require.False(t, got.Available)
	require.Equal(t, "1.2.3", got.CurrentVersion)
}

func TestUpdaterServiceCheckNowAvailable(t *testing.T) {
	silenceEmits(t)
	engine := &fakeEngine{rel: &updater.Release{
		Version: "1.3.0",
		Notes:   "new stuff",
	}}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.Nop())
	s.Attach(engine)

	info, err := s.CheckNow(t.Context())
	require.NoError(t, err)
	require.True(t, info.Available)
	require.Equal(t, "1.3.0", info.LatestVersion)
	require.Equal(t, ReleaseURL("1.3.0"), info.ReleaseURL)
	// Status reflects the cached result.
	require.True(t, s.Status().Available)
}

func TestUpdaterServiceCheckNowUpToDate(t *testing.T) {
	silenceEmits(t)
	engine := &fakeEngine{rel: nil}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.Nop())
	s.Attach(engine)

	info, err := s.CheckNow(t.Context())
	require.NoError(t, err)
	require.False(t, info.Available)
	require.Equal(t, "1.2.3", info.CurrentVersion)
}

func TestUpdaterServiceCheckNowError(t *testing.T) {
	silenceEmits(t)
	engine := &fakeEngine{checkErr: errors.New("boom")}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.Nop())
	s.Attach(engine)

	_, err := s.CheckNow(t.Context())
	require.Error(t, err)
}

func TestUpdaterServiceDevGate(t *testing.T) {
	silenceEmits(t)
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	// No engine attached => dev build.
	s := NewUpdaterService("dev", true, time.Millisecond, zerolog.Nop())

	info, err := s.CheckNow(t.Context())
	require.NoError(t, err)
	require.False(t, info.Available)

	// SetEnabled persists but starts no ticker (engine nil), so it must not
	// panic or spin.
	require.NoError(t, s.SetEnabled(true))
	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.NotNil(t, got.AutoUpdate)
	require.True(t, *got.AutoUpdate)
	s.Stop() // safe no-op
}

func TestUpdaterServiceTickerLifecycle(t *testing.T) {
	silenceEmits(t)
	synctest.Test(t, func(t *testing.T) {
		t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
		engine := &fakeEngine{rel: nil}
		s := NewUpdaterService("1.2.3", false, time.Minute, zerolog.Nop())
		s.Attach(engine)

		// Enabling checks immediately (initial check) then on each tick.
		require.NoError(t, s.SetEnabled(true))
		synctest.Wait()
		require.Equal(t, 1, engine.checkCount(), "initial check on enable")

		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, engine.checkCount(), "one check per interval")

		// Disabling stops the ticker: no further checks.
		require.NoError(t, s.SetEnabled(false))
		synctest.Wait()
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, engine.checkCount(), "no checks after disable")
	})
}

func TestUpdaterServiceInstallUpdate(t *testing.T) {
	silenceEmits(t)
	engine := &fakeEngine{}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.Nop())
	s.Attach(engine)

	require.NoError(t, s.InstallUpdate(t.Context()))
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.Equal(t, 1, engine.installs)
	require.Equal(t, 1, engine.restarts)
}

func TestUpdaterServiceInstallUpdateLogsDownloadFailure(t *testing.T) {
	silenceEmits(t)
	var logs bytes.Buffer
	engine := &fakeEngine{installErr: errors.New("checksum mismatch")}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.New(&logs))
	s.Attach(engine)
	s.available = &UpdateInfo{LatestVersion: "1.3.0"}

	err := s.InstallUpdate(t.Context())
	require.ErrorContains(t, err, "checksum mismatch")
	require.Contains(t, logs.String(), `"stage":"download_install"`)
	require.Contains(t, logs.String(), `"current_version":"1.2.3"`)
	require.Contains(t, logs.String(), `"latest_version":"1.3.0"`)
	require.Contains(t, logs.String(), `"message":"update install failed"`)
}

func TestUpdaterServiceInstallUpdateLogsRestartFailure(t *testing.T) {
	silenceEmits(t)
	var logs bytes.Buffer
	engine := &fakeEngine{restartErr: errors.New("helper failed")}
	s := NewUpdaterService("1.2.3", false, time.Hour, zerolog.New(&logs))
	s.Attach(engine)

	err := s.InstallUpdate(t.Context())
	require.ErrorContains(t, err, "helper failed")
	require.Contains(t, logs.String(), `"stage":"restart"`)
	require.Contains(t, logs.String(), `"message":"update install failed"`)
}

func TestUpdaterServiceInstallUpdateDevNoop(t *testing.T) {
	silenceEmits(t)
	s := NewUpdaterService("dev", false, time.Hour, zerolog.Nop())
	require.NoError(t, s.InstallUpdate(t.Context()))
}
