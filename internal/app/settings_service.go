package app

import (
	"context"
	"strings"
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
	// lookPath resolves an editor command against the subprocess PATH
	// (execenv.Resolver.LookPath); nil falls back to this process's own PATH.
	lookPath func(context.Context, string) (string, error)
}

// newSettingsService builds the service. producer and fetchers are nil in
// mock mode, where persistence still works and there is simply nothing live
// to apply a change to.
func newSettingsService(store *settings.Store, producer *ingest.Producer, fetchers *ghsource.Fetchers, lookPath func(context.Context, string) (string, error)) *SettingsService {
	return &SettingsService{store: store, producer: producer, fetchers: fetchers, lookPath: lookPath}
}

// NewSettingsService builds a settings-only view of the core's settings
// service, over the same *settings.Store App itself reads and writes. It
// exists for a driven port the adapter must construct before App does:
// app.Config's notification Gate is one of the two arguments New itself
// needs, so it cannot wait for core.Settings to exist. Nothing built this way
// calls SetGithub, so a nil producer and fetchers cost it nothing.
func NewSettingsService(store *settings.Store) *SettingsService {
	return newSettingsService(store, nil, nil, nil)
}

// Keybindings returns the persisted shortcut overrides keyed by command id.
// A nil map is normalized to an empty one so callers never null-check it.
func (s *SettingsService) Keybindings(context.Context) (map[string][]string, error) {
	cfg, err := s.store.Effective()
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

// AppearanceSettings is the persisted presentation configuration. The string
// values are opaque here: the frontend owns each valid set and heals unknown
// values, so "" means "nothing persisted" rather than an error.
type AppearanceSettings struct {
	Theme                  string
	TerminalFontSize       string
	TerminalFontFamily     string
	TerminalFontWeight     int
	TerminalFontWeightBold int
	TerminalLineHeight     float64
	TerminalLetterSpacing  int
	TerminalShowWindows    bool
	TerminalPoolSize       int
}

func (s *SettingsService) Appearance(context.Context) (AppearanceSettings, error) {
	cfg, err := s.store.Effective()
	if err != nil {
		return AppearanceSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	return AppearanceSettings{
		Theme:                  cfg.Appearance.Theme,
		TerminalFontSize:       cfg.Appearance.TerminalFontSize,
		TerminalFontFamily:     cfg.Appearance.TerminalFontFamily,
		TerminalFontWeight:     cfg.Appearance.TerminalFontWeight,
		TerminalFontWeightBold: cfg.Appearance.TerminalFontWeightBold,
		TerminalLineHeight:     cfg.Appearance.TerminalLineHeight,
		TerminalLetterSpacing:  cfg.Appearance.TerminalLetterSpacing,
		TerminalShowWindows:    cfg.Appearance.TerminalShowWindows,
		TerminalPoolSize:       cfg.Appearance.TerminalPoolSize,
	}, nil
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

func (s *SettingsService) SetTerminalFontFamily(_ context.Context, family string) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalFontFamily = family
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// SetTerminalFontWeights writes both weights at once: they are picked together
// in one control, and a normal weight above the bold one is the kind of state
// two independent setters would let a caller land in.
func (s *SettingsService) SetTerminalFontWeights(_ context.Context, weight, weightBold int) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalFontWeight = weight
		current.Appearance.TerminalFontWeightBold = weightBold
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

func (s *SettingsService) SetTerminalLineHeight(_ context.Context, lineHeight float64) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalLineHeight = lineHeight
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

func (s *SettingsService) SetTerminalLetterSpacing(_ context.Context, spacing int) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalLetterSpacing = spacing
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

func (s *SettingsService) SetTerminalShowWindows(_ context.Context, show bool) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalShowWindows = show
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

func (s *SettingsService) SetTerminalPoolSize(_ context.Context, size int) error {
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Appearance.TerminalPoolSize = size
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// ExperimentalSettings are the ships-dark opt-ins (ADR 0037). Each flag is
// read once at startup, so a persisted change applies on the next launch.
type ExperimentalSettings struct {
	Terminal bool
	Agents   bool
}

func (s *SettingsService) Experimental(context.Context) (ExperimentalSettings, error) {
	cfg, err := s.store.Effective()
	if err != nil {
		return ExperimentalSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	return ExperimentalSettings{Terminal: cfg.Experimental.Terminal, Agents: cfg.Experimental.Agents}, nil
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

// SetExperimentalAgents persists the Agents-area opt-in and returns the
// effective value after any process environment override is reapplied. Like
// SetExperimentalTerminal, the surfaces it gates are mounted at composition
// time, so the running app is unchanged until the next launch.
func (s *SettingsService) SetExperimentalAgents(_ context.Context, enabled bool) (bool, error) {
	effective, err := s.store.Update(func(current *settings.Settings) error {
		current.Experimental.Agents = enabled
		return nil
	})
	if err != nil {
		return false, Wrap(err, KindInternal, "saving settings")
	}
	return effective.Experimental.Agents, nil
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
	cfg, err := s.store.Effective()
	if err != nil {
		return NotificationSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	return NotificationSettings{
		Enabled:  cfg.Notifications.Enabled,
		Delivery: cfg.Notifications.Delivery,
		Sound:    cfg.Notifications.Sound,
	}, nil
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

// SetUpdatesEnabled persists the user's value and returns the effective value
// after any process environment override is reapplied.
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

func (s *SettingsService) Github(context.Context) (GithubSettings, error) {
	cfg, err := s.store.Effective()
	if err != nil {
		return GithubSettings{}, Wrap(err, KindInternal, "reading settings")
	}
	return GithubSettings{PollInterval: cfg.Polling.Interval.Duration(), MinPollInterval: settings.MinPollInterval}, nil
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
	interval := effective.Polling.Interval.Duration()
	if s.producer != nil {
		s.producer.SetInterval(interval)
	}
	if s.fetchers != nil {
		s.fetchers.SetSearchTTL(interval)
	}
	return nil
}

// Editor returns the configured editor command, or "" when none is set.
func (s *SettingsService) Editor(context.Context) (string, error) {
	cfg, err := s.store.Effective()
	if err != nil {
		return "", Wrap(err, KindInternal, "reading settings")
	}
	return cfg.Editor.Command, nil
}

// SetEditor persists the editor command. Empty clears the setting; anything
// else must be a single word — a command name or path, never a command line —
// the same rule agent commands follow (ADR 0061).
func (s *SettingsService) SetEditor(_ context.Context, command string) error {
	command = strings.TrimSpace(command)
	if len(strings.Fields(command)) > 1 {
		return Errorf(KindInvalid, "the editor command must be a single word, without flags")
	}
	_, err := s.store.Update(func(current *settings.Settings) error {
		current.Editor.Command = command
		return nil
	})
	return Wrap(err, KindInternal, "saving settings")
}

// EditorChoices reports the known editor catalogue with each command resolved
// against the subprocess PATH, for the Settings selector.
func (s *SettingsService) EditorChoices(ctx context.Context) []EditorChoice {
	return detectEditors(ctx, s.lookPath)
}
