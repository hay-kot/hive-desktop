package main

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
)

// SettingsService exposes user-tunable desktop settings to the frontend.
// Saving persists settings.yaml and applies supported values to the running
// services immediately.
type SettingsService struct {
	producer *ingest.Producer
	fetcher  *feed.LiveProvider
	logger   zerolog.Logger
}

// NewSettingsService constructs the Wails settings binding. producer and
// fetcher are nil in mock mode, while settings persistence remains available.
func NewSettingsService(producer *ingest.Producer, fetcher *feed.LiveProvider, logger zerolog.Logger) *SettingsService {
	return &SettingsService{producer: producer, fetcher: fetcher, logger: logger}
}

// GithubSettings is the GitHub integration's editable configuration.
type GithubSettings struct {
	PollIntervalSeconds    int `json:"pollIntervalSeconds"`
	MinPollIntervalSeconds int `json:"minPollIntervalSeconds"`
}

// NotificationSettings is the desktop notification configuration resolved
// from settings.yaml. Delivery is carried as a resolved string from the
// closed set settings.DeliveryAuto/DeliverySystem/DeliveryApp.
type NotificationSettings struct {
	NotificationsEnabled bool `json:"notificationsEnabled"`
	// Delivery is where an eligible notification is surfaced: "auto" (an OS
	// banner only while Hive is unfocused), "system" (always an OS banner), or
	// "app" (always in-app).
	Delivery          string `json:"delivery"`
	NotificationSound bool   `json:"notificationSound"`
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
	cfg, err := settings.LoadSettings()
	if err != nil {
		return KeybindingSettings{}, err
	}
	overrides := cfg.Keybindings
	if overrides == nil {
		overrides = map[string][]string{}
	}
	return KeybindingSettings{Overrides: overrides}, nil
}

// SetKeybindingSettings persists the shortcut overrides while preserving all
// unrelated desktop settings. An empty map clears the section entirely, which
// is how "reset everything to defaults" is expressed.
func (s *SettingsService) SetKeybindingSettings(in KeybindingSettings) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return err
	}
	if len(in.Overrides) == 0 {
		current.Keybindings = nil
	} else {
		current.Keybindings = in.Overrides
	}
	return settings.SaveSettings(current)
}

// AppearanceSettings returns the persisted appearance configuration. An empty
// Theme tells the frontend no choice has been recorded, which is its cue to
// adopt whatever theme its localStorage cache already holds.
func (s *SettingsService) AppearanceSettings() (AppearanceSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return AppearanceSettings{}, err
	}
	return AppearanceSettings{Theme: cfg.Appearance.Theme}, nil
}

// SetAppearanceSettings persists the appearance configuration while preserving
// all unrelated desktop settings.
func (s *SettingsService) SetAppearanceSettings(in AppearanceSettings) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return err
	}
	current.Appearance.Theme = in.Theme
	return settings.SaveSettings(current)
}

// NotificationSettings returns the current resolved notification settings.
func (s *SettingsService) NotificationSettings() (NotificationSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return NotificationSettings{}, err
	}
	return NotificationSettings{
		NotificationsEnabled: cfg.NotificationsEnabledOrDefault(),
		Delivery:             cfg.NotificationDeliveryOrDefault(),
		NotificationSound:    cfg.NotificationSoundOrDefault(),
	}, nil
}

// SetNotificationSettings persists the notification configuration while
// preserving all unrelated desktop settings.
func (s *SettingsService) SetNotificationSettings(in NotificationSettings) error {
	current, err := settings.LoadSettings()
	if err != nil {
		return err
	}
	current.NotificationsEnabled = &in.NotificationsEnabled
	// Persist the resolved mode: an unknown value from a stale frontend heals
	// to the default here rather than being written back verbatim.
	current.NotificationDelivery = settings.ResolveNotificationDelivery(in.Delivery)
	current.NotificationSound = &in.NotificationSound
	return settings.SaveSettings(current)
}

// GithubSettings returns the current resolved GitHub polling settings.
func (s *SettingsService) GithubSettings() (GithubSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return GithubSettings{}, err
	}
	interval, err := cfg.PollIntervalOrDefault(feed.DefaultPollInterval)
	if err != nil {
		return GithubSettings{}, err
	}
	return GithubSettings{
		PollIntervalSeconds:    int(interval / time.Second),
		MinPollIntervalSeconds: int(settings.MinPollInterval / time.Second),
	}, nil
}

// SetGithubSettings validates, persists, and immediately applies the GitHub
// poll interval. API callers below the floor are rejected rather than clamped.
func (s *SettingsService) SetGithubSettings(in GithubSettings) error {
	minimum := int(settings.MinPollInterval / time.Second)
	if in.PollIntervalSeconds < minimum {
		return fmt.Errorf("poll interval must be at least %d seconds", minimum)
	}
	if uint64(in.PollIntervalSeconds) > uint64((time.Duration(1<<63-1))/time.Second) {
		return fmt.Errorf("poll interval is too large")
	}

	interval := time.Duration(in.PollIntervalSeconds) * time.Second
	// Load-modify-save so unrelated fields (e.g. AutoUpdate) are preserved
	// rather than clobbered by writing a fresh, single-field Settings value.
	current, err := settings.LoadSettings()
	if err != nil {
		return err
	}
	current.PollInterval = interval.String()
	if err := settings.SaveSettings(current); err != nil {
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
