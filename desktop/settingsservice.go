package main

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/desktop"
	"github.com/hay-kot/hive-desktop/internal/desktop/feed"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline"
)

// SettingsService exposes user-tunable desktop settings to the frontend.
// Saving persists settings.yaml and applies supported values to the running
// services immediately.
type SettingsService struct {
	producer *pipeline.Producer
	fetcher  *feed.LiveProvider
	logger   zerolog.Logger
}

// NewSettingsService constructs the Wails settings binding. producer and
// fetcher are nil in mock mode, while settings persistence remains available.
func NewSettingsService(producer *pipeline.Producer, fetcher *feed.LiveProvider, logger zerolog.Logger) *SettingsService {
	return &SettingsService{producer: producer, fetcher: fetcher, logger: logger}
}

// GithubSettings is the GitHub integration's editable configuration.
type GithubSettings struct {
	PollIntervalSeconds    int `json:"pollIntervalSeconds"`
	MinPollIntervalSeconds int `json:"minPollIntervalSeconds"`
}

// NotificationSettings is the desktop notification configuration resolved
// from settings.yaml. All fields are explicit booleans for the frontend.
type NotificationSettings struct {
	NotificationsEnabled       bool `json:"notificationsEnabled"`
	SystemNotificationsEnabled bool `json:"systemNotificationsEnabled"`
	NotificationSound          bool `json:"notificationSound"`
}

// AppearanceSettings is the frontend's presentation configuration. Theme is
// carried verbatim: the frontend owns the valid set and heals unknown values,
// so an empty Theme means "nothing persisted yet" rather than an error.
type AppearanceSettings struct {
	Theme string `json:"theme"`
}

// KeybindingSettings carries keyboard shortcut overrides keyed by command id.
// Like AppearanceSettings the values are opaque to Go: the frontend owns the
// command vocabulary and the combo grammar, so this is transport only.
//
// An id absent from Overrides keeps its catalog default; an id mapped to an
// empty list is explicitly unbound.
type KeybindingSettings struct {
	Overrides map[string][]string `json:"overrides"`
}

// KeybindingSettings returns the persisted shortcut overrides. A nil map is
// normalized to an empty one so the frontend never has to null-check it.
func (s *SettingsService) KeybindingSettings() (KeybindingSettings, error) {
	settings, err := desktop.LoadSettings()
	if err != nil {
		return KeybindingSettings{}, err
	}
	overrides := settings.Keybindings
	if overrides == nil {
		overrides = map[string][]string{}
	}
	return KeybindingSettings{Overrides: overrides}, nil
}

// SetKeybindingSettings persists the shortcut overrides while preserving all
// unrelated desktop settings. An empty map clears the section entirely, which
// is how "reset everything to defaults" is expressed.
func (s *SettingsService) SetKeybindingSettings(settings KeybindingSettings) error {
	current, err := desktop.LoadSettings()
	if err != nil {
		return err
	}
	if len(settings.Overrides) == 0 {
		current.Keybindings = nil
	} else {
		current.Keybindings = settings.Overrides
	}
	return desktop.SaveSettings(current)
}

// AppearanceSettings returns the persisted appearance configuration. An empty
// Theme tells the frontend no choice has been recorded, which is its cue to
// adopt whatever theme its localStorage cache already holds.
func (s *SettingsService) AppearanceSettings() (AppearanceSettings, error) {
	settings, err := desktop.LoadSettings()
	if err != nil {
		return AppearanceSettings{}, err
	}
	return AppearanceSettings{Theme: settings.Appearance.Theme}, nil
}

// SetAppearanceSettings persists the appearance configuration while preserving
// all unrelated desktop settings.
func (s *SettingsService) SetAppearanceSettings(settings AppearanceSettings) error {
	current, err := desktop.LoadSettings()
	if err != nil {
		return err
	}
	current.Appearance.Theme = settings.Theme
	return desktop.SaveSettings(current)
}

// NotificationSettings returns the current resolved notification settings.
func (s *SettingsService) NotificationSettings() (NotificationSettings, error) {
	settings, err := desktop.LoadSettings()
	if err != nil {
		return NotificationSettings{}, err
	}
	return NotificationSettings{
		NotificationsEnabled:       settings.NotificationsEnabledOrDefault(),
		SystemNotificationsEnabled: settings.SystemNotificationsEnabledOrDefault(),
		NotificationSound:          settings.NotificationSoundOrDefault(),
	}, nil
}

// SetNotificationSettings persists the notification configuration while
// preserving all unrelated desktop settings.
func (s *SettingsService) SetNotificationSettings(settings NotificationSettings) error {
	current, err := desktop.LoadSettings()
	if err != nil {
		return err
	}
	current.NotificationsEnabled = &settings.NotificationsEnabled
	current.SystemNotificationsEnabled = &settings.SystemNotificationsEnabled
	current.NotificationSound = &settings.NotificationSound
	return desktop.SaveSettings(current)
}

// GithubSettings returns the current resolved GitHub polling settings.
func (s *SettingsService) GithubSettings() (GithubSettings, error) {
	settings, err := desktop.LoadSettings()
	if err != nil {
		return GithubSettings{}, err
	}
	interval, err := settings.PollIntervalOrDefault(feed.DefaultPollInterval)
	if err != nil {
		return GithubSettings{}, err
	}
	return GithubSettings{
		PollIntervalSeconds:    int(interval / time.Second),
		MinPollIntervalSeconds: int(desktop.MinPollInterval / time.Second),
	}, nil
}

// SetGithubSettings validates, persists, and immediately applies the GitHub
// poll interval. API callers below the floor are rejected rather than clamped.
func (s *SettingsService) SetGithubSettings(settings GithubSettings) error {
	minimum := int(desktop.MinPollInterval / time.Second)
	if settings.PollIntervalSeconds < minimum {
		return fmt.Errorf("poll interval must be at least %d seconds", minimum)
	}
	if uint64(settings.PollIntervalSeconds) > uint64((time.Duration(1<<63-1))/time.Second) {
		return fmt.Errorf("poll interval is too large")
	}

	interval := time.Duration(settings.PollIntervalSeconds) * time.Second
	// Load-modify-save so unrelated fields (e.g. AutoUpdate) are preserved
	// rather than clobbered by writing a fresh, single-field Settings value.
	current, err := desktop.LoadSettings()
	if err != nil {
		return err
	}
	current.PollInterval = interval.String()
	if err := desktop.SaveSettings(current); err != nil {
		return err
	}
	if s.producer != nil {
		s.producer.SetInterval(interval)
	}
	if s.fetcher != nil {
		s.fetcher.SetSearchTTL(interval)
	}
	return nil
}
