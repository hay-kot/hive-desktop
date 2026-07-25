package app

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
)

// SettingsService owns settings.yaml: reading the resolved values, validating
// a change, persisting it, and applying what can be applied to the running
// subsystems without a restart.
//
// Every setter is load-modify-save so unrelated fields survive; writing a
// fresh single-field Settings would clobber them.
type SettingsService struct {
	producer *ingest.Producer
	fetcher  *feed.LiveProvider
}

// newSettingsService builds the service. producer and fetcher are nil in mock
// mode, where persistence still works and there is simply nothing live to
// apply a change to.
func newSettingsService(producer *ingest.Producer, fetcher *feed.LiveProvider) *SettingsService {
	return &SettingsService{producer: producer, fetcher: fetcher}
}

// Keybindings returns the persisted shortcut overrides keyed by command id.
// A nil map is normalized to an empty one so callers never null-check it.
func (s *SettingsService) Keybindings(context.Context) (map[string][]string, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return nil, Wrap(err, KindInternal, "reading settings")
	}
	if cfg.Keybindings == nil {
		return map[string][]string{}, nil
	}
	return cfg.Keybindings, nil
}

// SetKeybindings persists shortcut overrides. An empty map clears the section
// entirely, which is how "reset everything to defaults" is expressed.
func (s *SettingsService) SetKeybindings(_ context.Context, overrides map[string][]string) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return Wrap(err, KindInternal, "reading settings")
	}
	if len(overrides) == 0 {
		current.Keybindings = nil
	} else {
		current.Keybindings = overrides
	}
	return Wrap(settings.SaveSettings(current), KindInternal, "saving settings")
}

// Theme returns the persisted theme, or "" when nothing has been recorded.
// The value is opaque here: the frontend owns the valid set and heals unknown
// values.
func (s *SettingsService) Theme(context.Context) (string, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return "", Wrap(err, KindInternal, "reading settings")
	}
	return cfg.Appearance.Theme, nil
}

func (s *SettingsService) SetTheme(_ context.Context, theme string) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return Wrap(err, KindInternal, "reading settings")
	}
	current.Appearance.Theme = theme
	return Wrap(settings.SaveSettings(current), KindInternal, "saving settings")
}

// NotificationSettings is the resolved notification configuration.
type NotificationSettings struct {
	Enabled bool
	// Delivery is where an eligible notification surfaces: "auto" (an OS
	// banner only while the app is unfocused), "system", or "app".
	Delivery string
	Sound    bool
}

func (s *SettingsService) Notifications(context.Context) (NotificationSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return NotificationSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	return NotificationSettings{
		Enabled:  cfg.NotificationsEnabledOrDefault(),
		Delivery: cfg.NotificationDeliveryOrDefault(),
		Sound:    cfg.NotificationSoundOrDefault(),
	}, nil
}

func (s *SettingsService) SetNotifications(_ context.Context, in NotificationSettings) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return Wrap(err, KindInternal, "reading settings")
	}
	current.NotificationsEnabled = &in.Enabled
	// Persist the resolved mode: an unknown value from a stale caller heals to
	// the default here rather than being written back verbatim.
	current.NotificationDelivery = settings.ResolveNotificationDelivery(in.Delivery)
	current.NotificationSound = &in.Sound
	return Wrap(settings.SaveSettings(current), KindInternal, "saving settings")
}

// GithubSettings is the GitHub integration's polling configuration. It is
// carried as a Duration: the seconds encoding is a wire concern.
type GithubSettings struct {
	PollInterval    time.Duration
	MinPollInterval time.Duration
}

func (s *SettingsService) Github(context.Context) (GithubSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return GithubSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	interval, err := cfg.PollIntervalOrDefault(feed.DefaultPollInterval)
	if err != nil {
		return GithubSettings{}, Wrap(err, KindInvalid, "the configured poll interval is not a duration")
	}
	return GithubSettings{PollInterval: interval, MinPollInterval: settings.MinPollInterval}, nil
}

// SetGithub validates against the floor, persists, and applies to the running
// producer and fetch layer. A caller below the floor is rejected rather than
// clamped: an API caller asking to poll every second should be told no, where
// a settings.yaml written by hand is clamped at load.
func (s *SettingsService) SetGithub(_ context.Context, in GithubSettings) error {
	if in.PollInterval < settings.MinPollInterval {
		return Errorf(KindInvalid, "poll interval must be at least %d seconds", int(settings.MinPollInterval/time.Second))
	}

	current, err := settings.LoadSettings()
	if err != nil {
		return Wrap(err, KindInternal, "reading settings")
	}
	current.PollInterval = in.PollInterval.String()
	if err := settings.SaveSettings(current); err != nil {
		return Wrap(err, KindInternal, "saving settings")
	}

	if s.producer != nil {
		s.producer.SetInterval(in.PollInterval)
	}
	if s.fetcher != nil {
		s.fetcher.SetSearchTTL(in.PollInterval)
	}
	return nil
}
