package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// MenuBarService is the Settings ▸ Menu bar pane's view of the pinned feeds.
type MenuBarService struct {
	menuBar *app.MenuBarService
}

func NewMenuBarService(m *app.MenuBarService) *MenuBarService {
	return &MenuBarService{menuBar: m}
}

// MenuBarLimits are the bounds the pane enforces before Go does.
type MenuBarLimits struct {
	MaxFeeds         int `json:"maxFeeds"`
	DefaultItemLimit int `json:"defaultItemLimit"`
	MaxItemLimit     int `json:"maxItemLimit"`
}

func (s *MenuBarService) Limits() MenuBarLimits {
	return MenuBarLimits{
		MaxFeeds:         settings.MaxMenuBarFeeds,
		DefaultItemLimit: settings.DefaultMenuBarItemLimit,
		MaxItemLimit:     settings.MaxMenuBarItemLimit,
	}
}

func (s *MenuBarService) Pins(ctx context.Context) ([]app.MenuBarPin, error) {
	return s.menuBar.Pins(ctx)
}

func (s *MenuBarService) SetPins(ctx context.Context, pins []app.MenuBarPin) error {
	return s.menuBar.SetPins(ctx, pins)
}

func (s *MenuBarService) FeedChoices(ctx context.Context) []app.MenuBarFeedChoice {
	return s.menuBar.FeedChoices(ctx)
}
