package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// settingsFileName is the desktop settings file, resolved under the desktop
// config root next to profiles.yaml, flows/, and actions.yml.
const settingsFileName = "settings.yaml"

// MinPollInterval is the floor for the configured poll interval. It matches
// GitHub's notifications polling contract.
const MinPollInterval = 60 * time.Second

// DefaultWebhookPort is the local webhook listener's default TCP port
// ("HIVE" on a phone keypad). The listener always binds 127.0.0.1.
const DefaultWebhookPort = 4483

// Release channels form a closed set (docs/decisions/0004): a version's
// prerelease identifier routes a build to its channel, and the updater follows
// exactly one of these.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
	ChannelDev    = "dev"
)

// Appearance holds presentation preferences owned by the frontend. Go stores
// these as opaque strings: the closed set of valid values (and healing of an
// unrecognized one) lives with the CSS that implements them, in the frontend's
// useTheme. Validating here would duplicate that list and would make an older
// build reject a settings.yaml written by a newer one.
type Appearance struct {
	// Theme is the selected frontend theme id, e.g. "dark". An empty value
	// means the frontend has never persisted a choice, which is what triggers
	// the one-time adoption of a pre-existing localStorage theme.
	Theme string `yaml:"theme,omitempty"`
}

// Settings holds user-tunable desktop behavior. Zero-valued fields mean use
// the application's default.
type Settings struct {
	PollInterval string `yaml:"poll_interval,omitempty"`
	// AutoUpdate toggles the desktop app's self-update checks. It is a pointer
	// so an absent key (nil) is distinguishable from an explicit `false`: unset
	// means "use the default" (on), matching the omitempty convention used for
	// PollInterval. Resolve it through AutoUpdateOrDefault rather than reading
	// the pointer directly.
	AutoUpdate *bool `yaml:"auto_update,omitempty"`
	// NotificationsEnabled controls Hive notifications. An absent key defaults
	// to enabled so upgrades preserve the prior behavior.
	NotificationsEnabled *bool `yaml:"notifications_enabled,omitempty"`
	// SystemNotificationsEnabled controls OS banners while Hive is unfocused.
	// It defaults to enabled when absent.
	SystemNotificationsEnabled *bool `yaml:"system_notifications_enabled,omitempty"`
	// NotificationSound controls sound for OS notification banners. It defaults
	// to enabled when absent.
	NotificationSound *bool `yaml:"notification_sound,omitempty"`
	// UpdateChannel pins the release channel the updater follows: "stable",
	// "beta", or "dev" (docs/decisions/0004). An absent key defaults to the
	// channel implied by the running build's own version, so beta/dev builds
	// track their channel without configuration. Resolve through
	// UpdateChannelOrDefault rather than reading the field directly.
	UpdateChannel string `yaml:"update_channel,omitempty"`
	// Appearance holds frontend presentation preferences. It is a value (not a
	// pointer) because an absent section and an empty one are equivalent: both
	// mean "no choice persisted yet". omitempty keeps the section out of
	// settings.yaml until something is actually set.
	Appearance Appearance `yaml:"appearance,omitempty"`
	// WebhookPort is the local webhook listener's TCP port. Absent or
	// out-of-range values fall back to DefaultWebhookPort; resolve through
	// WebhookPortOrDefault rather than reading the field directly.
	WebhookPort int `yaml:"webhook_port,omitempty"`
}

// SettingsPath is the settings.yaml location under the desktop config root.
func SettingsPath() string {
	return filepath.Join(ConfigDir(), settingsFileName)
}

// LoadSettings reads settings.yaml. Missing settings are equivalent to all
// defaults; malformed YAML and invalid duration values are reported so callers
// can warn before falling back to defaults.
func LoadSettings() (Settings, error) {
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("read desktop settings: %w", err)
	}

	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("parse desktop settings: %w", err)
	}
	if _, err := settings.PollIntervalOrDefault(MinPollInterval); err != nil {
		return Settings{}, fmt.Errorf("parse desktop settings poll interval: %w", err)
	}
	return settings, nil
}

// SaveSettings writes settings.yaml, creating its parent directory.
func SaveSettings(settings Settings) error {
	path := SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create desktop settings dir: %w", err)
	}
	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal desktop settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write desktop settings: %w", err)
	}
	return nil
}

// AutoUpdateOrDefault resolves AutoUpdate, defaulting to true (auto-update on)
// when the key is absent from settings.yaml.
func (s Settings) AutoUpdateOrDefault() bool {
	if s.AutoUpdate == nil {
		return true
	}
	return *s.AutoUpdate
}

// NotificationsEnabledOrDefault resolves NotificationsEnabled, defaulting to
// true when the key is absent from settings.yaml.
func (s Settings) NotificationsEnabledOrDefault() bool {
	if s.NotificationsEnabled == nil {
		return true
	}
	return *s.NotificationsEnabled
}

// SystemNotificationsEnabledOrDefault resolves SystemNotificationsEnabled,
// defaulting to true when the key is absent from settings.yaml.
func (s Settings) SystemNotificationsEnabledOrDefault() bool {
	if s.SystemNotificationsEnabled == nil {
		return true
	}
	return *s.SystemNotificationsEnabled
}

// NotificationSoundOrDefault resolves NotificationSound, defaulting to true
// when the key is absent from settings.yaml.
func (s Settings) NotificationSoundOrDefault() bool {
	if s.NotificationSound == nil {
		return true
	}
	return *s.NotificationSound
}

// UpdateChannelOrDefault resolves UpdateChannel against the closed channel
// set. Absent or unrecognized values fall back to fallback (mirroring
// PollIntervalOrDefault's tolerance for hand-edited input) so a typo can
// never mint a channel.
func (s Settings) UpdateChannelOrDefault(fallback string) string {
	switch s.UpdateChannel {
	case ChannelStable, ChannelBeta, ChannelDev:
		return s.UpdateChannel
	default:
		return fallback
	}
}

// WebhookPortOrDefault resolves WebhookPort, tolerating hand-edited values
// outside the valid port range by falling back to DefaultWebhookPort. The
// EnvWebhookPort environment variable, when set to a valid port, wins over
// the settings file outright (mirroring the EnvFlowsDir-style overrides) so
// parallel dev/e2e instances can each claim a distinct port.
func (s Settings) WebhookPortOrDefault() int {
	if v := os.Getenv(EnvWebhookPort); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	if s.WebhookPort > 0 && s.WebhookPort <= 65535 {
		return s.WebhookPort
	}
	return DefaultWebhookPort
}

// PollIntervalOrDefault resolves PollInterval. Hand-edited values below the
// floor are tolerated and clamped; callers that accept user input should
// reject them before saving.
func (s Settings) PollIntervalOrDefault(fallback time.Duration) (time.Duration, error) {
	if s.PollInterval == "" {
		return fallback, nil
	}
	interval, err := time.ParseDuration(s.PollInterval)
	if err != nil {
		return 0, err
	}
	if interval < MinPollInterval {
		return MinPollInterval, nil
	}
	return interval, nil
}
