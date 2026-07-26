package wailsui

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type trayProfile struct {
	ID      string
	Label   string
	Enabled bool
	Valid   bool
}

// trayProfiles projects a flow listing onto the tray's checkbox rows: a valid
// flow shows its name, an invalid one is disabled and labeled with its id so
// a broken flow file stays visible instead of vanishing from the menu.
//
// It takes []FlowSummary — the same DTO FlowsService.ListFlows returns to the
// frontend — rather than reading *flow.FlowStore itself. The tray used to
// call store.Statuses() directly and re-derive this exact projection by hand;
// that was a second, independent read of "is this flow valid, is it
// enabled", drifting from the one the frontend's listing already computes.
func trayProfiles(summaries []FlowSummary) []trayProfile {
	profiles := make([]trayProfile, 0, len(summaries))
	for _, s := range summaries {
		if s.Valid {
			profiles = append(profiles, trayProfile{ID: s.ID, Label: s.Name, Enabled: s.Enabled, Valid: true})
			continue
		}
		profiles = append(profiles, trayProfile{ID: s.ID, Label: s.ID + " (invalid)"})
	}
	return profiles
}

// ProfileTray owns the dynamic native tray menu. Profile rows are checkboxes:
// checked profiles poll and run, while unchecked profiles retain their feed
// data without executing. Invalid flow files remain visible but non-interactive.
//
// It goes through FlowsService rather than a raw *flow.FlowStore: toggling a
// checkbox is the same "enable/disable a flow" operation the frontend
// performs, and FlowsService.SetFlowEnabled already wraps the typed error and
// publishes the flows-updated event that this tray's own Refresh subscribes
// to (see Subscribe in events.go, and buildTray in ui.go) — a second,
// hand-rolled notification path here would just race the first.
type ProfileTray struct {
	app    *application.App
	flows  *FlowsService
	logger zerolog.Logger
	show   func()
	quit   func()
	tray   *application.SystemTray
	mu     sync.Mutex
	active bool
}

func NewProfileTray(
	app *application.App,
	flows *FlowsService,
	logger zerolog.Logger,
	icon []byte,
	show func(),
	quit func(),
) *ProfileTray {
	result := &ProfileTray{
		app:    app,
		flows:  flows,
		logger: logger,
		show:   show,
		quit:   quit,
		active: true,
	}
	result.tray = app.SystemTray.New().SetTemplateIcon(icon)
	result.Refresh()
	return result
}

// Refresh replaces the tray menu from the current flow listing. Wails
// marshals SetMenu onto the native UI thread after app startup.
func (t *ProfileTray) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.tray.SetMenu(t.menu())
}

// Close prevents filesystem watcher callbacks from touching the native tray
// once Wails begins tearing down its UI loop.
func (t *ProfileTray) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
}

func (t *ProfileTray) menu() *application.Menu {
	menu := t.app.NewMenu()
	menu.Add("Show Hive").OnClick(func(*application.Context) { t.show() })
	menu.AddSeparator()

	summaries, err := t.flows.ListFlows(context.Background())
	if err != nil {
		t.logger.Warn().Err(err).Msg("tray: listing flows failed")
	}
	for _, profile := range trayProfiles(summaries) {
		item := menu.AddCheckbox(profile.Label, profile.Enabled).SetEnabled(profile.Valid)
		if !profile.Valid {
			continue
		}
		id := profile.ID
		enabled := !profile.Enabled
		item.OnClick(func(*application.Context) {
			if _, err := t.flows.SetFlowEnabled(context.Background(), id, enabled); err != nil {
				t.logger.Warn().Err(err).Str("profile", id).Msg("tray: updating profile enablement failed")
			}
		})
	}

	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { t.quit() })
	return menu
}
