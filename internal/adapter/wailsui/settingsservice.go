package wailsui

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// SettingsService exposes user-tunable settings to the frontend. The JSON
// shapes below are the wire contract; the core deals in Durations and plain
// maps.
type SettingsService struct {
	settings *app.SettingsService
}

func NewSettingsService(s *app.SettingsService) *SettingsService {
	return &SettingsService{settings: s}
}

// GithubSettings is the GitHub integration's editable configuration. Seconds
// rather than a duration string: this is the wire shape the frontend edits.
type GithubSettings struct {
	PollIntervalSeconds    int `json:"pollIntervalSeconds"`
	MinPollIntervalSeconds int `json:"minPollIntervalSeconds"`
}

// NotificationSettings is the desktop notification configuration. Delivery is
// carried as a resolved string from the closed set auto/system/app.
type NotificationSettings struct {
	NotificationsEnabled bool `json:"notificationsEnabled"`
	// Delivery is where an eligible notification is surfaced: "auto" (an OS
	// banner only while Hive is unfocused), "system" (always an OS banner), or
	// "app" (always in-app).
	Delivery          string `json:"delivery"`
	NotificationSound bool   `json:"notificationSound"`
}

// AppearanceSettings is the frontend's presentation configuration. String
// values are carried verbatim: the frontend owns each valid set and heals
// unknown values, so an empty field means "nothing persisted yet" rather than
// an error.
type AppearanceSettings struct {
	Theme string `json:"theme"`
	// TerminalFontSize is a preset name (small/medium/large/xl/xxl), not a
	// pixel count — the frontend owns the mapping.
	TerminalFontSize string `json:"terminalFontSize"`
	// TerminalShowWindows lists every active session's tmux windows in the
	// terminal sidebar, not just the attached session's. Ships on.
	TerminalShowWindows bool `json:"terminalShowWindows"`
	// TerminalPoolSize is how many sessions the terminal view keeps attached
	// for instant switching (ADR 0042). Carried verbatim; the frontend heals
	// anything outside 1-6 to the default, 3.
	TerminalPoolSize int `json:"terminalPoolSize"`
}

// ExperimentalSettings carries the ships-dark opt-ins (ADR 0037). Terminal is
// the effective persisted value, not the running one: the flag is read at
// startup, so the frontend compares it against TerminalService.Enabled to
// know whether a relaunch is pending.
type ExperimentalSettings struct {
	Terminal bool `json:"terminal"`
}

// KeybindingSettings carries keyboard shortcut overrides keyed by command id.
// Like AppearanceSettings the values are opaque to Go: the frontend owns the
// command vocabulary and the combo grammar.
//
// An id absent from Overrides keeps its catalog default; an id mapped to an
// empty list is explicitly unbound.
type KeybindingSettings struct {
	Overrides map[string][]string `json:"overrides"`
}

func (s *SettingsService) KeybindingSettings(ctx context.Context) (KeybindingSettings, error) {
	overrides, err := s.settings.Keybindings(ctx)
	if err != nil {
		return KeybindingSettings{}, err
	}
	return KeybindingSettings{Overrides: overrides}, nil
}

func (s *SettingsService) SetKeybindingSettings(ctx context.Context, in KeybindingSettings) error {
	return s.settings.SetKeybindings(ctx, in.Overrides)
}

func (s *SettingsService) AppearanceSettings(ctx context.Context) (AppearanceSettings, error) {
	current, err := s.settings.Appearance(ctx)
	if err != nil {
		return AppearanceSettings{}, err
	}
	return AppearanceSettings{
		Theme:               current.Theme,
		TerminalFontSize:    current.TerminalFontSize,
		TerminalShowWindows: current.TerminalShowWindows,
		TerminalPoolSize:    current.TerminalPoolSize,
	}, nil
}

// The appearance setters are per-field so the theme picker and the terminal
// font picker cannot clobber each other's persisted value.
func (s *SettingsService) SetTheme(ctx context.Context, theme string) error {
	return s.settings.SetTheme(ctx, theme)
}

func (s *SettingsService) SetTerminalFontSize(ctx context.Context, size string) error {
	return s.settings.SetTerminalFontSize(ctx, size)
}

func (s *SettingsService) SetTerminalShowWindows(ctx context.Context, show bool) error {
	return s.settings.SetTerminalShowWindows(ctx, show)
}

func (s *SettingsService) SetTerminalPoolSize(ctx context.Context, size int) error {
	return s.settings.SetTerminalPoolSize(ctx, size)
}

func (s *SettingsService) ExperimentalSettings(ctx context.Context) (ExperimentalSettings, error) {
	current, err := s.settings.Experimental(ctx)
	if err != nil {
		return ExperimentalSettings{}, err
	}
	return ExperimentalSettings{Terminal: current.Terminal}, nil
}

func (s *SettingsService) SetExperimentalTerminal(ctx context.Context, enabled bool) (ExperimentalSettings, error) {
	effective, err := s.settings.SetExperimentalTerminal(ctx, enabled)
	if err != nil {
		return ExperimentalSettings{}, err
	}
	return ExperimentalSettings{Terminal: effective}, nil
}

func (s *SettingsService) NotificationSettings(ctx context.Context) (NotificationSettings, error) {
	current, err := s.settings.Notifications(ctx)
	if err != nil {
		return NotificationSettings{}, err
	}
	return NotificationSettings{
		NotificationsEnabled: current.Enabled,
		Delivery:             current.Delivery,
		NotificationSound:    current.Sound,
	}, nil
}

func (s *SettingsService) SetNotificationSettings(ctx context.Context, in NotificationSettings) error {
	return s.settings.SetNotifications(ctx, app.NotificationSettings{
		Enabled:  in.NotificationsEnabled,
		Delivery: in.Delivery,
		Sound:    in.NotificationSound,
	})
}

func (s *SettingsService) GithubSettings(ctx context.Context) (GithubSettings, error) {
	current, err := s.settings.Github(ctx)
	if err != nil {
		return GithubSettings{}, err
	}
	return GithubSettings{
		PollIntervalSeconds:    int(current.PollInterval / time.Second),
		MinPollIntervalSeconds: int(current.MinPollInterval / time.Second),
	}, nil
}

// SetGithubSettings converts the wire's seconds to a duration and hands it to
// the core, which owns the floor and the live apply. A negative or absurd
// value is caught here because it cannot be represented as a duration at all.
func (s *SettingsService) SetGithubSettings(ctx context.Context, in GithubSettings) error {
	if in.PollIntervalSeconds < 0 {
		return app.Errorf(app.KindInvalid, "poll interval must not be negative")
	}
	if uint64(in.PollIntervalSeconds) > uint64(time.Duration(1<<63-1)/time.Second) {
		return app.Errorf(app.KindInvalid, "poll interval is too large")
	}
	return s.settings.SetGithub(ctx, app.GithubSettings{
		PollInterval: time.Duration(in.PollIntervalSeconds) * time.Second,
	})
}
