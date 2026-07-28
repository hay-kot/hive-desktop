package settings

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

const settingsFileName = "settings.yaml"

const (
	MinPollInterval = 60 * time.Second
	// MaxDebugPause prevents a stale development setting from making startup
	// appear permanently hung while still allowing deliberate crash-window tests.
	MaxDebugPause = 10 * time.Minute
)

const (
	WebhookPortMin = 20000
	WebhookPortMax = 32767
)

var reservedWebhookPorts = map[int]bool{
	20000: true, 22000: true, 24800: true, 25565: true, 26257: true,
	27017: true, 27018: true, 27019: true, 28017: true, 29418: true,
	31337: true, 32400: true,
}

const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
	ChannelDev    = "dev"

	DeliveryAuto   = "auto"
	DeliverySystem = "system"
	DeliveryApp    = "app"

	MockLive        = "live"
	MockFeed        = "feed"
	MockPipeline    = "pipeline"
	MockOnboarding  = "onboarding"
	MockActionSmoke = "action-smoke"
)

// Duration is a YAML- and environment-friendly Go duration.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }
func (d Duration) String() string          { return time.Duration(d).String() }

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

type PollingSettings struct {
	Interval Duration `yaml:"interval" env:"HIVE_DESKTOP_POLLING_INTERVAL"`
}

type UpdateSettings struct {
	Enabled bool   `yaml:"enabled"           env:"HIVE_DESKTOP_UPDATES_ENABLED"`
	Channel string `yaml:"channel,omitempty" env:"HIVE_DESKTOP_UPDATES_CHANNEL"`
}

type NotificationSettings struct {
	Enabled  bool   `yaml:"enabled"  env:"HIVE_DESKTOP_NOTIFICATIONS_ENABLED"`
	Delivery string `yaml:"delivery" env:"HIVE_DESKTOP_NOTIFICATIONS_DELIVERY"`
	Sound    bool   `yaml:"sound"    env:"HIVE_DESKTOP_NOTIFICATIONS_SOUND"`
}

type Appearance struct {
	Theme string `yaml:"theme,omitempty" env:"HIVE_DESKTOP_APPEARANCE_THEME"`
}

// HTTPSettings configures the local loopback HTTP server that hosts both the
// webhook listener and the agent API. On by default: it is loopback-only, so it
// is reachable only from this machine.
type HTTPSettings struct {
	Enabled bool   `yaml:"enabled" env:"HIVE_DESKTOP_HTTP_ENABLED"`
	Host    string `yaml:"host"    env:"HIVE_DESKTOP_HTTP_HOST"`
	Port    int    `yaml:"port"    env:"HIVE_DESKTOP_HTTP_PORT"`
}

type MockSettings struct {
	Mode string `yaml:"mode" env:"HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE"`
}

type InstanceSettings struct {
	ID string `yaml:"id,omitempty" env:"HIVE_DESKTOP_DEVELOPMENT_INSTANCE_ID"`
}

type ServerSettings struct {
	Host string `yaml:"host" env:"HOST"`
	Port int    `yaml:"port" env:"PORT"`
}

// PprofSettings gates the pprof endpoint; when enabled it mounts on the shared
// HTTP server (ADR 0023), so it has no host/port of its own.
type PprofSettings struct {
	Enabled bool `yaml:"enabled" env:"HIVE_DESKTOP_DEVELOPMENT_PPROF_ENABLED"`
}

type DebugSettings struct {
	PauseIngest Duration `yaml:"pause_ingest" env:"HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_INGEST"`
	PauseCommit Duration `yaml:"pause_commit" env:"HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_COMMIT"`
}

// EnvGitHubAPIBase is the environment name behind development.github.api_base.
// It is named here because startup reports whether the value it is running
// with came from the environment or from settings.yaml.
const EnvGitHubAPIBase = "HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE"

// GitHubDevSettings redirects the GitHub REST/GraphQL base at cmd/devserver,
// the development caching proxy and event simulator (ADR 0017). Empty — the
// shipped value — means api.github.com.
//
// Only the API base moves. The OAuth base stays github.com: a device-flow
// token exchange has no business passing through dev tooling, and it draws no
// rate-limit budget, so redirecting it would be all risk and no benefit.
type GitHubDevSettings struct {
	APIBase string `yaml:"api_base,omitempty" env:"HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE"`
}

type DevelopmentSettings struct {
	Mocks    MockSettings      `yaml:"mocks"`
	Instance InstanceSettings  `yaml:"instance,omitempty"`
	GitHub   GitHubDevSettings `yaml:"github,omitempty"`
	Vite     ServerSettings    `yaml:"vite"               envPrefix:"HIVE_DESKTOP_DEVELOPMENT_VITE_"`
	Wails    ServerSettings    `yaml:"wails"              envPrefix:"HIVE_DESKTOP_DEVELOPMENT_WAILS_"`
	Pprof    PprofSettings     `yaml:"pprof"`
	Debug    DebugSettings     `yaml:"debug"`
}

// Settings is the typed settings.yaml schema. Environment override provenance
// is process-local and is never serialized.
type Settings struct {
	Version       int                  `yaml:"version"`
	Polling       PollingSettings      `yaml:"polling"`
	Updates       UpdateSettings       `yaml:"updates"`
	Notifications NotificationSettings `yaml:"notifications"`
	Appearance    Appearance           `yaml:"appearance,omitempty"`
	HTTP          HTTPSettings         `yaml:"http"`
	Keybindings   map[string][]string  `yaml:"keybindings,omitempty"`
	Development   DevelopmentSettings  `yaml:"development"`

	overrides map[string]bool
}

// SettingsSchemaVersion reports the settings.yaml schema version this build
// reads and writes. It reads configmigrate.SettingsSet.Current at call time
// rather than a frozen const, so a test injecting a higher Current is
// reflected everywhere. A legacy file with no `version:` key is treated as
// the Set's baseline (v1).
func SettingsSchemaVersion() int { return configmigrate.SettingsSet.Current }

func DefaultSettings() Settings {
	return Settings{
		Version:       configmigrate.SettingsSet.Current,
		Polling:       PollingSettings{Interval: Duration(5 * time.Minute)},
		Updates:       UpdateSettings{Enabled: true},
		Notifications: NotificationSettings{Enabled: true, Delivery: DeliveryAuto, Sound: true},
		HTTP:          HTTPSettings{Enabled: true, Host: "127.0.0.1", Port: 0},
		Development: DevelopmentSettings{
			Mocks: MockSettings{Mode: MockLive},
			Vite:  ServerSettings{Host: "127.0.0.1", Port: 0},
			Wails: ServerSettings{Host: "127.0.0.1", Port: 0},
			Pprof: PprofSettings{Enabled: false},
		},
	}
}

func (s Settings) EnvironmentOverridden(name string) bool { return s.overrides[name] }
func (s Settings) MockMode() string {
	if s.Development.Mocks.Mode == MockLive {
		return ""
	}
	return s.Development.Mocks.Mode
}

// GitHubAPIBase returns the canonical API base override, or "" for the
// client's own api.github.com default. The trailing slash is trimmed because
// the client concatenates paths onto this value directly.
func (s Settings) GitHubAPIBase() string {
	return normalizeAPIBase(s.Development.GitHub.APIBase)
}

func normalizeAPIBase(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

func ResolveNotificationDelivery(value string) string {
	switch value {
	case DeliveryAuto, DeliverySystem, DeliveryApp:
		return value
	default:
		return DeliveryAuto
	}
}

func (s Settings) Validate() error {
	if s.Polling.Interval.Duration() < MinPollInterval {
		return fmt.Errorf("polling.interval must be at least %s", MinPollInterval)
	}
	switch s.Updates.Channel {
	case "", ChannelStable, ChannelBeta, ChannelDev:
	default:
		return fmt.Errorf("updates.channel must be stable, beta, or dev")
	}
	switch s.Notifications.Delivery {
	case DeliveryAuto, DeliverySystem, DeliveryApp:
	default:
		return fmt.Errorf("notifications.delivery must be auto, system, or app")
	}
	if !validListenerHost(s.HTTP.Host) {
		return fmt.Errorf("http.host must be a loopback address")
	}
	if !ValidListenerPort(s.HTTP.Port) {
		return fmt.Errorf("http.port must be 0 or between 1024 and 65535")
	}
	switch s.Development.Mocks.Mode {
	case MockLive, MockFeed, MockPipeline, MockOnboarding, MockActionSmoke:
	default:
		return fmt.Errorf("development.mocks.mode is invalid")
	}
	// Wails constructs the frontend development URL with localhost rather than
	// the configured Vite host. Binding Vite to another loopback address (for
	// example 127.0.0.2) therefore starts successfully but leaves Wails unable
	// to reach it. Keep the known-working explicit IPv4 loopback until Wails can
	// consume the host as part of that URL.
	if s.Development.Vite.Host != "127.0.0.1" || !ValidListenerPort(s.Development.Vite.Port) {
		return fmt.Errorf("development.vite.host must be 127.0.0.1 and port must be 0 or between 1024 and 65535")
	}
	if !validServer(s.Development.Wails) {
		return fmt.Errorf("development.wails must use a loopback host and port must be 0 or between 1024 and 65535")
	}
	if s.Development.Debug.PauseIngest < 0 || s.Development.Debug.PauseCommit < 0 {
		return fmt.Errorf("development debug pauses must not be negative")
	}
	if s.Development.Debug.PauseIngest.Duration() > MaxDebugPause || s.Development.Debug.PauseCommit.Duration() > MaxDebugPause {
		return fmt.Errorf("development debug pauses must not exceed %s", MaxDebugPause)
	}
	if s.Development.Instance.ID != "" && strings.ContainsAny(s.Development.Instance.ID, `/\\`) {
		return fmt.Errorf("development.instance.id must not contain path separators")
	}
	// Loopback-only, for the same reason the webhook listener is (ADR 0007):
	// this value redirects an authenticated GitHub client, so the only host
	// allowed to receive that traffic is one on this machine. Because it is
	// enforced here, it holds for a value arriving from settings.yaml and from
	// the environment alike — a persisted setting cannot aim the app at a
	// remote collector.
	if err := validateGitHubAPIBase(s.GitHubAPIBase()); err != nil {
		return err
	}
	return nil
}

func validateGitHubAPIBase(base string) error {
	if base == "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("development.github.api_base must be a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("development.github.api_base must use http or https")
	}
	if !validListenerHost(parsed.Hostname()) {
		return fmt.Errorf("development.github.api_base must point at a loopback host")
	}
	return nil
}

func validServer(server ServerSettings) bool {
	return validListenerHost(server.Host) && ValidListenerPort(server.Port)
}

func validListenerHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ValidListenerPort(port int) bool { return port == 0 || (port >= 1024 && port <= 65535) }

// Package-level helpers remain for isolated tests and e2e harnesses. Runtime
// composition owns and injects one Store at its resolved Paths.SettingsPath.
func LoadPersistedSettings() (Settings, error) { return NewStore(SettingsPath()).Persisted() }
func LoadSettings() (Settings, error)          { return NewStore(SettingsPath()).Effective() }

func SaveSettings(cfg Settings) error {
	settingsFileMu.Lock()
	defer settingsFileMu.Unlock()
	return saveSettingsAt(SettingsPath(), cfg)
}

// AllocateWebhookPort offers a stable candidate for the settings UI. Runtime
// automatic allocation binds port zero directly and does not use this probe.
func AllocateWebhookPort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	const attempts = 64
	span := WebhookPortMax - WebhookPortMin + 1
	for range attempts {
		port := WebhookPortMin + rand.IntN(span)
		if reservedWebhookPorts[port] {
			continue
		}
		ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port found in %d-%d after %d attempts", WebhookPortMin, WebhookPortMax, attempts)
}
