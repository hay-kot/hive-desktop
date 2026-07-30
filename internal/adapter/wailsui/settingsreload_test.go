package wailsui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// The update ticker is the only settings value this side of the app holds, so
// the reload hook is the only thing that adopts one — everything else the
// frontend re-reads for itself on settings:updated. Nothing else covers it.
func TestSettingsReloadHookAdoptsUpdatesEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(path, []byte("updates:\n  enabled: false\n"), 0o600))

	u := &UI{updater: NewUpdaterService("1.0.0", true, time.Minute, nil, zerolog.Nop())}
	hook := u.settingsReloadHook(app.NewSettingsService(settings.NewStore(path)))

	hook([]string{"polling.interval"})
	assert.True(t, u.updater.Status().Enabled, "a reload of an unrelated field leaves the ticker alone")

	hook([]string{"updates.enabled"})
	assert.False(t, u.updater.Status().Enabled, "the value the file now holds is adopted")
}
