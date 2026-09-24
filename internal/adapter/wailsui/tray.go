package wailsui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/hay-kot/hive-desktop/internal/app"
)

type trayProfile struct {
	ID    string
	Label string
	Valid bool
}

// trayProfiles projects a flow listing onto the tray's profile links: a valid
// flow shows its name, an invalid one is disabled and labeled with its id so
// a broken flow file stays visible instead of vanishing from the menu.
//
// It takes []FlowSummary — the same DTO FlowsService.ListFlows returns to the
// frontend — so the tray lists profiles in rail order through one projection.
func trayProfiles(summaries []FlowSummary) []trayProfile {
	profiles := make([]trayProfile, 0, len(summaries))
	for _, s := range summaries {
		if s.Valid {
			profiles = append(profiles, trayProfile{ID: s.ID, Label: s.Name, Valid: true})
			continue
		}
		profiles = append(profiles, trayProfile{ID: s.ID, Label: s.ID + " (invalid)"})
	}
	return profiles
}

// TrayDeps is what the menu bar tray reads and what its clicks call back into.
type TrayDeps struct {
	App     *application.App
	Flows   *FlowsService
	MenuBar *app.MenuBarService
	Inbox   *app.InboxService
	Sources *app.SourcesService
	Logger  zerolog.Logger
	// TemplateIcon is the macOS/Windows tintable mark, LinuxIcon the
	// pre-coloured one (see applyTrayIcon).
	TemplateIcon, LinuxIcon []byte
	Show                    func()
	Quit                    func()
}

// MenuBarTray owns the dynamic native tray menu: the pinned feeds and their
// items, and links that open each profile in Hive.
type MenuBarTray struct {
	deps   TrayDeps
	tray   *application.SystemTray
	mu     sync.Mutex
	active bool
	// lastPolled is the poll time the current menu shows; the watcher
	// rebuilds when the producer moves past it.
	lastPolled time.Time
	stop       chan struct{}
}

// trayPollCheckInterval bounds how stale the footer's poll time can get. A
// poll that changes nothing publishes no event, so the tray has to look.
const trayPollCheckInterval = 30 * time.Second

func NewMenuBarTray(deps TrayDeps) *MenuBarTray {
	result := &MenuBarTray{deps: deps, active: true, stop: make(chan struct{})}
	result.tray = applyTrayIcon(deps.App.SystemTray.New(), deps.TemplateIcon, deps.LinuxIcon)
	result.Refresh()
	go result.watchPolls()
	return result
}

// applyTrayIcon installs the tray icon using the platform's icon semantics.
// macOS tints a template icon to match the menu bar in both appearances. Linux
// has no template concept — wails' StatusNotifierItem backend pushes the bytes
// to the panel as a raw pixmap — so it takes the pre-coloured white mark
// instead; the black template would be invisible on a dark panel.
func applyTrayIcon(tray *application.SystemTray, templateIcon, linuxIcon []byte) *application.SystemTray {
	if runtime.GOOS == "linux" {
		return tray.SetIcon(linuxIcon)
	}
	return tray.SetTemplateIcon(templateIcon)
}

// Refresh replaces the tray menu from current state. Wails marshals SetMenu
// onto the native UI thread after app startup.
func (t *MenuBarTray) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.tray.SetMenu(t.menu())
}

// Close prevents event and watcher callbacks from touching the native tray
// once Wails begins tearing down its UI loop.
func (t *MenuBarTray) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active {
		close(t.stop)
	}
	t.active = false
}

func (t *MenuBarTray) watchPolls() {
	ticker := time.NewTicker(trayPollCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			polled := t.deps.MenuBar.LastPolled()
			t.mu.Lock()
			stale := !polled.Equal(t.lastPolled)
			t.mu.Unlock()
			if stale {
				t.Refresh()
			}
		}
	}
}

// menu builds the whole menu. Caller holds t.mu.
func (t *MenuBarTray) menu() *application.Menu {
	menu := t.deps.App.NewMenu()
	menu.Add("Show Hive").OnClick(func(*application.Context) { t.deps.Show() })
	menu.AddSeparator()

	snapshot, err := t.deps.MenuBar.Snapshot(context.Background())
	if err != nil {
		t.deps.Logger.Warn().Err(err).Msg("tray: reading the menu bar snapshot failed")
	}
	t.lastPolled = snapshot.LastPolled

	if len(snapshot.Pinned) == 0 {
		menu.Add("Pin feeds to the menu bar…").OnClick(func(*application.Context) {
			t.open(MenuBarNavigation{Settings: true})
		})
		menu.AddSeparator()
	}
	for _, feed := range snapshot.Pinned {
		t.addPinnedFeed(menu, feed)
		menu.AddSeparator()
	}

	menu.Add("Refresh").OnClick(func(*application.Context) { t.refreshSources() })
	t.addProfiles(menu.AddSubmenu("Profiles"))
	menu.AddSeparator()

	menu.Add(trayUpdated(snapshot.LastPolled, time.Now())).SetEnabled(false)
	menu.Add("Quit").OnClick(func(*application.Context) { t.deps.Quit() })
	return menu
}

func (t *MenuBarTray) addPinnedFeed(menu *application.Menu, feed app.MenuBarFeedView) {
	feedNav := MenuBarNavigation{ProfileID: feed.ProfileID, FeedID: feed.Feed}
	menu.Add(trayFeedPath(feed.MenuBarFeedName)).OnClick(func(*application.Context) { t.open(feedNav) })
	if len(feed.Items) == 0 {
		menu.Add("Nothing here").SetBitmap(trayBlankMark).SetEnabled(false)
		return
	}
	for _, item := range feed.Items {
		mark := trayBlankMark
		if item.Unread {
			mark = trayUnreadMark
		}
		row := application.NewSubMenuItem(truncateRunes(item.Title, trayTitleLimit)).SetBitmap(mark)
		menu.Append(application.NewMenuFromItems(row))
		t.addItemActions(row.GetSubmenu(), feed.ProfileID, item)
	}
	if more := feed.Total - int64(len(feed.Items)); more > 0 {
		menu.Add(fmt.Sprintf("%d more…", more)).SetBitmap(trayBlankMark).OnClick(func(*application.Context) { t.open(feedNav) })
	}
}

func (t *MenuBarTray) addItemActions(sub *application.Menu, profileID string, item app.MenuBarItem) {
	itemNav := MenuBarNavigation{ProfileID: profileID, ItemID: item.ID}
	if item.URL != "" {
		url := item.URL
		sub.Add("Open in Browser").OnClick(func(*application.Context) {
			if err := t.deps.App.Browser.OpenURL(url); err != nil {
				t.deps.Logger.Warn().Err(err).Msg("tray: opening an item in the browser failed")
			}
		})
		sub.Add("Copy Link").OnClick(func(*application.Context) { t.deps.App.Clipboard.SetText(url) })
	}
	sub.Add("Open in Hive").OnClick(func(*application.Context) { t.open(itemNav) })
	if len(item.Actions) == 0 {
		return
	}
	sub.AddSeparator()
	for _, action := range item.Actions {
		sub.Add(action.Label).OnClick(func(*application.Context) { t.runAction(action, itemNav) })
	}
}

func (t *MenuBarTray) addProfiles(sub *application.Menu) {
	summaries, err := t.deps.Flows.ListFlows(context.Background())
	if err != nil {
		t.deps.Logger.Warn().Err(err).Msg("tray: listing flows failed")
	}
	for _, profile := range trayProfiles(summaries) {
		item := sub.Add(profile.Label).SetEnabled(profile.Valid)
		if profile.Valid {
			nav := MenuBarNavigation{ProfileID: profile.ID}
			item.OnClick(func(*application.Context) { t.open(nav) })
		}
	}
}

// runAction runs one item action from the menu. Anything the menu cannot
// finish on its own — a rerun that needs confirming, or a failure the user
// has to read — opens the item in Hive, where the detail pane can.
func (t *MenuBarTray) runAction(action app.MenuBarAction, item MenuBarNavigation) {
	ctx := context.Background()
	logger := t.deps.Logger.With().Str("action", action.ID).Int64("item", item.ItemID).Logger()
	if action.Clipboard {
		text, err := t.deps.Inbox.RenderClipboardAction(ctx, action.ID, []int64{item.ItemID}, nil)
		if err != nil {
			logger.Warn().Err(err).Msg("tray: rendering a clipboard action failed")
			t.open(item)
			return
		}
		t.deps.App.Clipboard.SetText(text)
		return
	}
	run, err := t.deps.Inbox.InvokeAction(ctx, app.InvokeActionRequest{ActionID: action.ID, ItemID: item.ItemID})
	if err != nil {
		logger.Warn().Err(err).Msg("tray: running an action failed")
		t.open(item)
		emitNotificationToast(NotificationToast{Title: action.Label + " failed", Body: err.Error(), Severity: "error"})
		return
	}
	if run.ConfirmationRequired {
		t.open(item)
	}
}

func (t *MenuBarTray) refreshSources() {
	if _, err := t.deps.Sources.Refresh(context.Background()); err != nil {
		t.deps.Logger.Warn().Err(err).Msg("tray: refreshing sources failed")
	}
	t.Refresh()
}

func (t *MenuBarTray) open(nav MenuBarNavigation) {
	t.deps.Show()
	emitMenuBarOpen(nav)
}

// trayFeedPath reads "Profile › Folder › Feed", the way the sidebar nests it.
// A feed outside any folder skips that segment.
func trayFeedPath(name app.MenuBarFeedName) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{name.ProfileName, name.Folder, name.Name} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " › ")
}

const trayTitleLimit = 60

func trayUpdated(polled, now time.Time) string {
	if polled.IsZero() {
		return "Not polled yet"
	}
	polled = polled.In(now.Location())
	if y, m, d := polled.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return "Updated at " + polled.Format("3:04 PM")
	}
	return "Updated " + polled.Format("Jan 2, 3:04 PM")
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}
