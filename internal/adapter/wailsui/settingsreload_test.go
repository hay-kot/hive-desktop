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

func TestSettingsReloadHookAdoptsUpdatesSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(path, []byte("updates:\n  enabled: false\n  channel: beta\n"), 0o600))

	channels := &recordingChannelSwapper{}
	u := &UI{updater: NewUpdaterService("1.0.0", true, time.Minute, nil, zerolog.Nop())}
	u.updater.Attach(&fakeEngine{}, channels, settings.ChannelStable)
	t.Cleanup(u.updater.Stop)
	hook := u.settingsReloadHook(app.NewSettingsService(settings.NewStore(path)))

	hook([]string{"polling.interval"})
	assert.True(t, u.updater.Status().Enabled, "a reload of an unrelated field leaves the ticker alone")
	assert.Empty(t, channels.currentChannel())

	hook([]string{"updates.channel"})
	assert.Equal(t, settings.ChannelBeta, channels.currentChannel())

	hook([]string{"updates.enabled"})
	assert.False(t, u.updater.Status().Enabled, "the value the file now holds is adopted")
}
