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
	settings       *app.SettingsService
	restartPending func(context.Context) []app.RestartPendingField
}

func NewSettingsService(s *app.SettingsService, restartPending func(context.Context) []app.RestartPendingField) *SettingsService {
	return &SettingsService{settings: s, restartPending: restartPending}
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

// AppearanceSettings is the frontend's presentation configuration. Values are
// carried verbatim: the frontend owns each valid set and heals unknown values,
// so an empty field means "nothing persisted yet" rather than an error.
type AppearanceSettings struct {
	Theme string `json:"theme"`
	// TerminalFontSize is a preset name (small/medium/large/xl/xxl), not a
	// pixel count — the frontend owns the mapping.
	TerminalFontSize string `json:"terminalFontSize"`
}

// ExperimentalSettings carries the ships-dark opt-ins (ADR 0037). Terminal is
// the effective persisted value, not the running one; RestartPending is what
// reports the difference.
type ExperimentalSettings struct {
	Terminal bool `json:"terminal"`
}

// RestartPendingField is one persisted value this process is not running.
// Field is the dotted settings path (or bootstrap.data_dir /
// bootstrap.config_dir), which is what a view keys off to decide where to
// surface the hint.
type RestartPendingField struct {
	Field     string `json:"field"`
	Reason    string `json:"reason"`
	Running   string `json:"running"`
	Persisted string `json:"persisted"`
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

func (s *SettingsService) KeybindingSettings(ctx context.Context) KeybindingSettings {
	return KeybindingSettings{Overrides: s.settings.Keybindings(ctx)}
}

// RestartPending lists everything persisted that this process is not running.
// It is the single answer behind every "restart needed" hint in the UI — the
// terminal opt-in, the HTTP listener, a moved data or config directory — so a
// new startup-only setting surfaces without a second comparison being written.
func (s *SettingsService) RestartPending(ctx context.Context) []RestartPendingField {
	pending := s.restartPending(ctx)
	out := make([]RestartPendingField, 0, len(pending))
	for _, field := range pending {
		out = append(out, RestartPendingField(field))
	}
	return out
}

func (s *SettingsService) SetKeybindingSettings(ctx context.Context, in KeybindingSettings) error {
	return s.settings.SetKeybindings(ctx, in.Overrides)
}

func (s *SettingsService) AppearanceSettings(ctx context.Context) AppearanceSettings {
	current := s.settings.Appearance(ctx)
	return AppearanceSettings{
		Theme:            current.Theme,
		TerminalFontSize: current.TerminalFontSize,
	}
}

// The appearance setters are per-field so the theme picker and the terminal
// font picker cannot clobber each other's persisted value.
func (s *SettingsService) SetTheme(ctx context.Context, theme string) error {
	return s.settings.SetTheme(ctx, theme)
}

func (s *SettingsService) SetTerminalFontSize(ctx context.Context, size string) error {
	return s.settings.SetTerminalFontSize(ctx, size)
}

func (s *SettingsService) ExperimentalSettings(ctx context.Context) ExperimentalSettings {
	return ExperimentalSettings{Terminal: s.settings.Experimental(ctx).Terminal}
}

func (s *SettingsService) SetExperimentalTerminal(ctx context.Context, enabled bool) (ExperimentalSettings, error) {
	effective, err := s.settings.SetExperimentalTerminal(ctx, enabled)
	if err != nil {
		return ExperimentalSettings{}, err
	}
	return ExperimentalSettings{Terminal: effective}, nil
}

func (s *SettingsService) NotificationSettings(ctx context.Context) NotificationSettings {
	current := s.settings.Notifications(ctx)
	return NotificationSettings{
		NotificationsEnabled: current.Enabled,
		Delivery:             current.Delivery,
		NotificationSound:    current.Sound,
	}
}

func (s *SettingsService) SetNotificationSettings(ctx context.Context, in NotificationSettings) error {
	return s.settings.SetNotifications(ctx, app.NotificationSettings{
		Enabled:  in.NotificationsEnabled,
		Delivery: in.Delivery,
		Sound:    in.NotificationSound,
	})
}

func (s *SettingsService) GithubSettings(ctx context.Context) GithubSettings {
	current := s.settings.Github(ctx)
	return GithubSettings{
		PollIntervalSeconds:    int(current.PollInterval / time.Second),
		MinPollIntervalSeconds: int(current.MinPollInterval / time.Second),
	}
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
