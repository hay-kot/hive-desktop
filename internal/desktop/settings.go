package desktop

import (
	"fmt"
	"math/rand/v2"
	"net"
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

// WebhookPortMin and WebhookPortMax bound the range a webhook port is drawn
// from on first run. The window sits above the crowded well-known/registered
// ports and below both Linux's (32768) and macOS's (49152) ephemeral floors,
// so a port persisted here is never handed out to an outbound connection
// while Hive is closed — the failure mode a port in the IANA dynamic range
// would invite. The listener always binds 127.0.0.1.
const (
	WebhookPortMin = 20000
	WebhookPortMax = 32767
)

// reservedWebhookPorts are registered services inside the generation range.
// Binding is what actually proves a port free, so this list only matters for
// a service that happens to be stopped at first run and would otherwise be a
// conflict waiting to happen the next time it starts.
var reservedWebhookPorts = map[int]bool{
	20000: true, // DNP
	22000: true, // Syncthing
	24800: true, // Synergy/Barrier
	25565: true, // Minecraft
	26257: true, // CockroachDB
	27017: true, // MongoDB
	27018: true, // MongoDB shard
	27019: true, // MongoDB config server
	28017: true, // MongoDB HTTP status
	29418: true, // Gerrit
	31337: true, // widely probed
	32400: true, // Plex
}

// Release channels form a closed set (docs/decisions/0004): a version's
// prerelease identifier routes a build to its channel, and the updater follows
// exactly one of these.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
	ChannelDev    = "dev"
)

// Notification delivery modes form a closed set: where an eligible
// notification is surfaced, independent of whether it is eligible at all
// (that remains NotificationsEnabled's job).
const (
	// DeliveryAuto shows an OS banner only while Hive is unfocused, and an
	// in-app toast while it is focused.
	DeliveryAuto = "auto"
	// DeliverySystem always shows an OS banner, focused or not.
	DeliverySystem = "system"
	// DeliveryApp never shows an OS banner; notifications stay in-app.
	DeliveryApp = "app"
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
	// NotificationDelivery selects how an eligible notification is surfaced:
	// DeliveryAuto (an OS banner only while Hive is unfocused), DeliverySystem
	// (always an OS banner), or DeliveryApp (always an in-app toast). An absent
	// key defaults to DeliveryAuto. Resolve through
	// NotificationDeliveryOrDefault rather than reading the field directly.
	NotificationDelivery string `yaml:"notification_delivery,omitempty"`
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
	// WebhookPort is the local webhook listener's TCP port. There is no fixed
	// default: an absent or out-of-range value means "not allocated yet", and
	// ResolveWebhookPort draws a random one and persists it. Resolve through
	// ResolveWebhookPort rather than reading the field directly.
	WebhookPort int `yaml:"webhook_port,omitempty"`
	// WebhookEnabled toggles the local webhook listener. It is read once at
	// startup — the listener binds a port and serves flow-declared routes, so
	// flipping it live would mean tearing down in-flight deliveries for no
	// benefit. Absent defaults to enabled; resolve through
	// WebhookEnabledOrDefault rather than reading the pointer directly.
	WebhookEnabled *bool `yaml:"webhook_enabled,omitempty"`
	// Keybindings holds keyboard shortcut *overrides*, keyed by the frontend's
	// bindable command id (desktop/frontend/src/keybindings/catalog.ts). An
	// absent id keeps its catalog default; an id mapped to an empty list is
	// explicitly unbound — the two are deliberately different, which is why
	// this is a sparse map rather than the full keymap.
	//
	// Go stores these opaquely, exactly as it stores Appearance.Theme and for
	// the same reason: the command vocabulary and the combo grammar live with
	// the frontend that implements them, so validating here would duplicate
	// that list and make an older build reject a settings.yaml written by a
	// newer one.
	Keybindings map[string][]string `yaml:"keybindings,omitempty"`
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

// NotificationDeliveryOrDefault resolves NotificationDelivery against the
// closed delivery set.
func (s Settings) NotificationDeliveryOrDefault() string {
	return ResolveNotificationDelivery(s.NotificationDelivery)
}

// ResolveNotificationDelivery maps value onto the closed delivery set, falling
// back to DeliveryAuto for an empty or unknown one (the same tolerance
// UpdateChannelOrDefault applies) so neither a hand-edited typo nor a stale
// frontend can mint a delivery mode. Exported so a writer can normalize before
// persisting rather than round-tripping an unknown value through settings.yaml.
func ResolveNotificationDelivery(value string) string {
	switch value {
	case DeliverySystem, DeliveryApp:
		return value
	default:
		return DeliveryAuto
	}
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

// WebhookEnabledOrDefault resolves WebhookEnabled, defaulting to true when
// the key is absent from settings.yaml.
func (s Settings) WebhookEnabledOrDefault() bool {
	if s.WebhookEnabled == nil {
		return true
	}
	return *s.WebhookEnabled
}

// ValidWebhookPort reports whether port is a usable listener port. Ports
// below 1024 are excluded outright: binding them needs privileges the desktop
// app does not have, so accepting one would only defer the failure to startup.
func ValidWebhookPort(port int) bool {
	return port >= 1024 && port <= 65535
}

// WebhookPortOverride returns the port EnvWebhookPort claims, or 0 when the
// variable is unset or unusable. The override wins over settings.yaml
// outright (mirroring the EnvFlowsDir-style overrides) so parallel dev/e2e
// instances can each claim a distinct port without writing to the user's
// settings.
func WebhookPortOverride() int {
	v := os.Getenv(EnvWebhookPort)
	if v == "" {
		return 0
	}
	port, err := strconv.Atoi(v)
	if err != nil || !ValidWebhookPort(port) {
		return 0
	}
	return port
}

// AllocateWebhookPort draws a random port from the generation range that is
// not reserved and binds on 127.0.0.1 right now. The bind is released before
// returning, so the result is a strong hint rather than a reservation — the
// listener still has to tolerate a bind failure, which it does.
func AllocateWebhookPort() (int, error) {
	const attempts = 64
	span := WebhookPortMax - WebhookPortMin + 1
	for range attempts {
		port := WebhookPortMin + rand.IntN(span)
		if reservedWebhookPorts[port] {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port found in %d-%d after %d attempts", WebhookPortMin, WebhookPortMax, attempts)
}

// ResolveWebhookPort returns the port the webhook listener should bind.
// EnvWebhookPort wins outright. Otherwise a valid persisted port is used
// as-is, and an absent or out-of-range one (first run, or a hand-edited
// value) is replaced by a freshly allocated random port that is written back
// to settings.yaml so the endpoint URLs users paste into sending systems stay
// stable across restarts.
func ResolveWebhookPort(settings Settings) (int, error) {
	if port := WebhookPortOverride(); port > 0 {
		return port, nil
	}
	if ValidWebhookPort(settings.WebhookPort) {
		return settings.WebhookPort, nil
	}

	port, err := AllocateWebhookPort()
	if err != nil {
		return 0, fmt.Errorf("allocate webhook port: %w", err)
	}
	// Load-modify-save rather than writing the passed-in value: the caller's
	// copy may predate an unrelated write.
	current, err := LoadSettings()
	if err != nil {
		return 0, err
	}
	current.WebhookPort = port
	if err := SaveSettings(current); err != nil {
		return 0, fmt.Errorf("persist allocated webhook port: %w", err)
	}
	return port, nil
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
