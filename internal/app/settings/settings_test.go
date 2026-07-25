package settings

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadSettings(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     time.Duration
		wantErr  bool
	}{
		{name: "missing file", want: time.Minute},
		{name: "valid interval", contents: "poll_interval: 2m\n", want: 2 * time.Minute},
		{name: "below floor is clamped", contents: "poll_interval: 10s\n", want: MinPollInterval},
		{name: "invalid duration", contents: "poll_interval: fast\n", wantErr: true},
		{name: "malformed yaml", contents: "poll_interval: [\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
			if tt.contents != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(SettingsPath()), 0o755))
				require.NoError(t, os.WriteFile(SettingsPath(), []byte(tt.contents), 0o600))
			}

			settings, err := LoadSettings()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			interval, err := settings.PollIntervalOrDefault(time.Minute)
			require.NoError(t, err)
			require.Equal(t, tt.want, interval)
		})
	}
}

func TestSaveSettingsRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigPath, filepath.Join(root, "nested", "config", "profiles.yaml"))
	want := Settings{PollInterval: "2m"}

	require.NoError(t, SaveSettings(want))
	require.FileExists(t, SettingsPath())
	got, err := LoadSettings()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestAutoUpdateOrDefault(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		settings Settings
		want     bool
	}{
		{name: "unset defaults to true", settings: Settings{}, want: true},
		{name: "explicit true", settings: Settings{AutoUpdate: &enabled}, want: true},
		{name: "explicit false", settings: Settings{AutoUpdate: &disabled}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.settings.AutoUpdateOrDefault())
		})
	}
}

func TestUpdateChannelOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		fallback string
		want     string
	}{
		{name: "unset uses fallback", settings: Settings{}, fallback: ChannelStable, want: ChannelStable},
		{name: "unset keeps prerelease fallback", settings: Settings{}, fallback: ChannelDev, want: ChannelDev},
		{name: "explicit beta wins over fallback", settings: Settings{UpdateChannel: ChannelBeta}, fallback: ChannelStable, want: ChannelBeta},
		{name: "unknown channel uses fallback", settings: Settings{UpdateChannel: "nightly"}, fallback: ChannelStable, want: ChannelStable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.settings.UpdateChannelOrDefault(tt.fallback))
		})
	}
}

func TestNotificationSettingsOrDefault(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		settings Settings
		want     bool
		resolve  func(Settings) bool
	}{
		{name: "notifications unset defaults to true", settings: Settings{}, want: true, resolve: Settings.NotificationsEnabledOrDefault},
		{name: "notifications explicit false", settings: Settings{NotificationsEnabled: &disabled}, want: false, resolve: Settings.NotificationsEnabledOrDefault},
		{name: "notification sound explicit true", settings: Settings{NotificationSound: &enabled}, want: true, resolve: Settings.NotificationSoundOrDefault},
		{name: "notification sound explicit false", settings: Settings{NotificationSound: &disabled}, want: false, resolve: Settings.NotificationSoundOrDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.resolve(tt.settings))
		})
	}
}

func TestNotificationDeliveryOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     string
	}{
		{name: "unset defaults to auto", settings: Settings{}, want: DeliveryAuto},
		{name: "explicit auto", settings: Settings{NotificationDelivery: DeliveryAuto}, want: DeliveryAuto},
		{name: "explicit system", settings: Settings{NotificationDelivery: DeliverySystem}, want: DeliverySystem},
		{name: "explicit app", settings: Settings{NotificationDelivery: DeliveryApp}, want: DeliveryApp},
		{name: "hand-edited typo heals to auto", settings: Settings{NotificationDelivery: "banner"}, want: DeliveryAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.settings.NotificationDeliveryOrDefault())
		})
	}
}

func TestSettingsAutoUpdateRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))

	// Unset auto_update in the file resolves to the default (on).
	require.NoError(t, os.MkdirAll(filepath.Dir(SettingsPath()), 0o755))
	require.NoError(t, os.WriteFile(SettingsPath(), []byte("poll_interval: 2m\n"), 0o600))
	got, err := LoadSettings()
	require.NoError(t, err)
	require.Nil(t, got.AutoUpdate)
	require.True(t, got.AutoUpdateOrDefault())

	// An explicit false persists and round-trips as false.
	disabled := false
	require.NoError(t, SaveSettings(Settings{PollInterval: "2m", AutoUpdate: &disabled}))
	got, err = LoadSettings()
	require.NoError(t, err)
	require.NotNil(t, got.AutoUpdate)
	require.False(t, got.AutoUpdateOrDefault())
	require.Equal(t, "2m", got.PollInterval)
}

func TestSettingsAppearanceRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))

	// An absent appearance section reads as no persisted theme, which is the
	// frontend's cue to adopt its localStorage value.
	require.NoError(t, os.MkdirAll(filepath.Dir(SettingsPath()), 0o755))
	require.NoError(t, os.WriteFile(SettingsPath(), []byte("poll_interval: 2m\n"), 0o600))
	got, err := LoadSettings()
	require.NoError(t, err)
	require.Empty(t, got.Appearance.Theme)

	// A theme persists, round-trips, and is written as a nested section so
	// appearance has room for the other presentation preferences.
	require.NoError(t, SaveSettings(Settings{PollInterval: "2m", Appearance: Appearance{Theme: "gruvbox"}}))
	got, err = LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "gruvbox", got.Appearance.Theme)
	require.Equal(t, "2m", got.PollInterval)

	contents, err := os.ReadFile(SettingsPath())
	require.NoError(t, err)
	require.Contains(t, string(contents), "appearance:\n    theme: gruvbox")
}

func TestSettingsAppearanceOmittedWhenUnset(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))

	require.NoError(t, SaveSettings(Settings{PollInterval: "2m"}))

	contents, err := os.ReadFile(SettingsPath())
	require.NoError(t, err)
	require.NotContains(t, string(contents), "appearance")
}

func TestWebhookPortOverride(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "unset", value: "", want: 0},
		{name: "valid", value: "4499", want: 4499},
		{name: "not a number", value: "not-a-port", want: 0},
		{name: "above range", value: "70000", want: 0},
		{name: "privileged", value: "80", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvWebhookPort, tt.value)
			require.Equal(t, tt.want, WebhookPortOverride())
		})
	}
}

func TestAllocateWebhookPort(t *testing.T) {
	port, err := AllocateWebhookPort(t.Context())
	require.NoError(t, err)
	require.GreaterOrEqual(t, port, WebhookPortMin)
	require.LessOrEqual(t, port, WebhookPortMax)
	require.False(t, reservedWebhookPorts[port], "allocated a reserved port")

	// The allocated port was bindable a moment ago and was released, so it
	// must still be claimable here.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	require.NoError(t, ln.Close())
}

func TestResolveWebhookPort(t *testing.T) {
	t.Run("env override wins without persisting", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
		t.Setenv(EnvWebhookPort, "4499")

		port, err := ResolveWebhookPort(t.Context(), Settings{WebhookPort: 9001})
		require.NoError(t, err)
		require.Equal(t, 4499, port)
		require.NoFileExists(t, SettingsPath())
	})

	t.Run("persisted port is used as-is", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
		t.Setenv(EnvWebhookPort, "")

		port, err := ResolveWebhookPort(t.Context(), Settings{WebhookPort: 9001})
		require.NoError(t, err)
		require.Equal(t, 9001, port)
	})

	t.Run("first run allocates and persists", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
		t.Setenv(EnvWebhookPort, "")
		require.NoError(t, SaveSettings(Settings{PollInterval: "2m"}))

		port, err := ResolveWebhookPort(t.Context(), Settings{PollInterval: "2m"})
		require.NoError(t, err)
		require.GreaterOrEqual(t, port, WebhookPortMin)
		require.LessOrEqual(t, port, WebhookPortMax)

		// Persisted, so the endpoint URL survives a restart — and unrelated
		// settings survived the write.
		saved, err := LoadSettings()
		require.NoError(t, err)
		require.Equal(t, port, saved.WebhookPort)
		require.Equal(t, "2m", saved.PollInterval)

		// A second resolve is stable rather than re-rolling.
		again, err := ResolveWebhookPort(t.Context(), saved)
		require.NoError(t, err)
		require.Equal(t, port, again)
	})

	t.Run("out-of-range persisted port is replaced", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(EnvConfigPath, filepath.Join(root, "config", "profiles.yaml"))
		t.Setenv(EnvWebhookPort, "")

		port, err := ResolveWebhookPort(t.Context(), Settings{WebhookPort: 70000})
		require.NoError(t, err)
		require.GreaterOrEqual(t, port, WebhookPortMin)
		require.LessOrEqual(t, port, WebhookPortMax)
	})
}

func TestWebhookEnabledOrDefault(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		settings Settings
		want     bool
	}{
		{name: "unset defaults to enabled", settings: Settings{}, want: true},
		{name: "explicit true", settings: Settings{WebhookEnabled: &enabled}, want: true},
		{name: "explicit false", settings: Settings{WebhookEnabled: &disabled}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.settings.WebhookEnabledOrDefault())
		})
	}
}

func TestValidWebhookPort(t *testing.T) {
	require.False(t, ValidWebhookPort(0))
	require.False(t, ValidWebhookPort(80), "privileged ports are not bindable by the app")
	require.False(t, ValidWebhookPort(1023))
	require.True(t, ValidWebhookPort(1024))
	require.True(t, ValidWebhookPort(65535))
	require.False(t, ValidWebhookPort(65536))
}
