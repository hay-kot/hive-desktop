package app

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
)

// SettingsService owns settings.yaml: reading the resolved values, validating
// a change, persisting it, and applying what can be applied to the running
// subsystems without a restart.
//
// Every setter is load-modify-save so unrelated fields survive; writing a
// fresh single-field Settings would clobber them.
type SettingsService struct {
	store    *settings.Store
	producer *ingest.Producer
	fetchers *ghsource.Fetchers
}

// newSettingsService builds the service. producer and fetchers are nil in
// mock mode, where persistence still works and there is simply nothing live
// to apply a change to.
func newSettingsService(store *settings.Store, producer *ingest.Producer, fetchers *ghsource.Fetchers) *SettingsService {
	return &SettingsService{store: store, producer: producer, fetchers: fetchers}
}

// NewSettingsService builds a settings-only view of the core's settings
// service, over the same *settings.Store App itself reads and writes. It
// exists for a driven port the adapter must construct before App does:
// app.Config's notification Gate is one of the two arguments New itself
// needs, so it cannot wait for core.Settings to exist. Nothing built this way
// calls SetGithub, so a nil producer and fetchers cost it nothing.
func NewSettingsService(store *settings.Store) *SettingsService {
	return newSettingsService(store, nil, nil)
}

// Keybindings returns the persisted shortcut overrides keyed by command id.
// A nil map is normalized to an empty one so callers never null-check it.
func (s *SettingsService) Keybindings(context.Context) map[string][]string {
	cfg := s.store.Current()
	if cfg.Keybindings == nil {
		return map[string][]string{}
	}
	return cfg.Keybindings
}

// SetKeybindings persists shortcut overrides. An empty map clears the section
// entirely, which is how "reset everything to defaults" is expressed.
func (s *SettingsService) SetKeybindings(_ context.Context, overrides map[string][]string) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		if len(overrides) == 0 {
			current.Keybindings = nil
		} else {
			current.Keybindings = overrides
		}
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// AppearanceSettings is the persisted presentation configuration. Values are
// opaque here: the frontend owns each valid set and heals unknown values, so
// "" means "nothing persisted" rather than an error.
type AppearanceSettings struct {
	Theme            string
	TerminalFontSize string
}

func (s *SettingsService) Appearance(context.Context) AppearanceSettings {
	cfg := s.store.Current()
	return AppearanceSettings{
		Theme:            cfg.Appearance.Theme,
		TerminalFontSize: cfg.Appearance.TerminalFontSize,
	}
}

func (s *SettingsService) SetTheme(_ context.Context, theme string) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.Theme = theme
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

func (s *SettingsService) SetTerminalFontSize(_ context.Context, size string) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalFontSize = size
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// ExperimentalSettings are the ships-dark opt-ins (ADR 0037). Each flag is
// read once at startup, so a persisted change applies on the next launch.
type ExperimentalSettings struct {
	Terminal bool
}

func (s *SettingsService) Experimental(context.Context) ExperimentalSettings {
	return ExperimentalSettings{Terminal: s.store.Current().Experimental.Terminal}
}

// SetExperimentalTerminal persists the opt-in and returns the effective value
// after any process environment override is reapplied. The surfaces it gates
// are mounted at composition time, so the running app is unchanged until the
// next launch.
func (s *SettingsService) SetExperimentalTerminal(_ context.Context, enabled bool) (bool, error) {
	effective, err := s.store.Update(func(current *settings.Settings) error {
		current.Experimental.Terminal = enabled
		return nil
	})
	if err != nil {
		return false, Wrap(err, KindInternal, "saving settings")
	}
	return effective.Experimental.Terminal, nil
}

// NotificationSettings is the resolved notification configuration.
type NotificationSettings struct {
	Enabled bool
	// Delivery is where an eligible notification surfaces: "auto" (an OS
	// banner only while the app is unfocused), "system", or "app".
	Delivery string
	Sound    bool
}

func (s *SettingsService) Notifications(context.Context) NotificationSettings {
	cfg := s.store.Current()
	return NotificationSettings{
		Enabled:  cfg.Notifications.Enabled,
		Delivery: cfg.Notifications.Delivery,
		Sound:    cfg.Notifications.Sound,
	}
}

func (s *SettingsService) SetNotifications(_ context.Context, in NotificationSettings) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Notifications.Enabled = in.Enabled
		current.Notifications.Delivery = settings.ResolveNotificationDelivery(in.Delivery)
		current.Notifications.Sound = in.Sound
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// GithubSettings is the GitHub integration's polling configuration. It is
// carried as a Duration: the seconds encoding is a wire concern.
type GithubSettings struct {
	PollInterval    time.Duration
	MinPollInterval time.Duration
}

// UpdatesSettings is the self-update configuration. Channel is empty when the
// build's own channel is in force.
type UpdatesSettings struct {
	Enabled bool
	Channel string
}

func (s *SettingsService) Updates(context.Context) UpdatesSettings {
	cfg := s.store.Current()
	return UpdatesSettings{Enabled: cfg.Updates.Enabled, Channel: cfg.Updates.Channel}
}

// SetUpdatesEnabled persists the user's value and returns the effective value
// after any process environment override is reapplied. Applying it — starting
// or stopping the poll ticker — belongs to the adapter that owns the engine.
func (s *SettingsService) SetUpdatesEnabled(enabled bool) (bool, error) {
	effective, err := s.store.Update(func(current *settings.Settings) error {
		current.Updates.Enabled = enabled
		return nil
	})
	if err != nil {
		return false, Wrap(err, KindInternal, "saving settings")
	}
	return effective.Updates.Enabled, nil
}

func (s *SettingsService) Github(context.Context) GithubSettings {
	return GithubSettings{
		PollInterval:    s.store.Current().Polling.Interval.Duration(),
		MinPollInterval: settings.MinPollInterval,
	}
}

// SetGithub validates against the floor, persists, and applies to the running
// producer and fetch layer. A caller below the floor is rejected rather than
// clamped: an API caller asking to poll every second should be told no, where
// a settings.yaml written by hand is clamped at load.
func (s *SettingsService) SetGithub(_ context.Context, in GithubSettings) error {
	if in.PollInterval < settings.MinPollInterval {
		return Errorf(KindInvalid, "poll interval must be at least %d seconds", int(settings.MinPollInterval/time.Second))
	}

	effective, err := s.store.Update(func(current *settings.Settings) error {
		current.Polling.Interval = settings.Duration(in.PollInterval)
		return nil
	})
	if err != nil {
		return Wrap(err, KindInternal, "saving settings")
	}
	s.applyPolling(effective.Polling.Interval.Duration())
	return nil
}

// applyPolling pushes an interval into the running producer and fetch layer.
// It is SetGithub's apply half, split out so a reload can adopt a hand-edited
// interval without persisting anything: a reload path that wrote the file back
// would retrigger the watcher that called it. Both are nil in mock mode, where
// persistence still works and there is nothing live to apply to.
func (s *SettingsService) applyPolling(interval time.Duration) {
	if s.producer != nil {
		s.producer.SetInterval(interval)
	}
	if s.fetchers != nil {
		s.fetchers.SetSearchTTL(interval)
	}
}
