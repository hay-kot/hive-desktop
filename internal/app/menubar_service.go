package app

import (
	"cmp"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// MenuBarService owns the feeds pinned to the menu bar and the read that
// renders them: each pinned feed's newest items with the actions a click can
// run on them.
type MenuBarService struct {
	settings *settings.Store
	flows    *flow.FlowStore
	items    *stores.InboxItemStore
	actions  *actions.ActionStore
	polls    LastTicker
	events   *events.Bus
}

// LastTicker reports when sources were last polled; zero means not yet.
type LastTicker interface {
	LastTick() time.Time
}

type MenuBarDeps struct {
	Settings *settings.Store
	Flows    *flow.FlowStore
	Items    *stores.InboxItemStore
	Catalog  *actions.ActionStore
	// Polls is nil in mock modes, which run no producer.
	Polls  LastTicker
	Events *events.Bus
}

func newMenuBarService(d MenuBarDeps) *MenuBarService {
	return &MenuBarService{settings: d.Settings, flows: d.Flows, items: d.Items, actions: d.Catalog, polls: d.Polls, events: d.Events}
}

// MenuBarPin is one pinned feed. Limit is always resolved, never zero.
type MenuBarPin struct {
	Feed  string `json:"feed"`
	Limit int    `json:"limit"`
}

// MenuBarFeedChoice is a feed that can be pinned.
type MenuBarFeedChoice struct {
	Feed string `json:"feed"`
	MenuBarFeedName
}

// MenuBarFeedName is where a feed sits: its profile, the sidebar folder
// holding it (empty at the top level), and its own name.
type MenuBarFeedName struct {
	ProfileName string `json:"profileName"`
	Folder      string `json:"folder"`
	Name        string `json:"name"`
}

type MenuBarSnapshot struct {
	Pinned     []MenuBarFeedView `json:"pinned"`
	LastPolled time.Time         `json:"lastPolled"`
}

type MenuBarFeedView struct {
	ProfileID string `json:"profileId"`
	Feed      string `json:"feed"`
	MenuBarFeedName
	Unread int64         `json:"unread"`
	Total  int64         `json:"total"`
	Items  []MenuBarItem `json:"items"`
}

// MenuBarItem carries the forge fields a source may put in its payload
// (repo, number, notification reason); a source without them leaves them
// empty and the item renders by title alone.
type MenuBarItem struct {
	ID      int64           `json:"id"`
	Title   string          `json:"title"`
	URL     string          `json:"url"`
	Unread  bool            `json:"unread"`
	Kind    string          `json:"kind"`
	Repo    string          `json:"repo"`
	Number  int             `json:"number"`
	Reason  string          `json:"reason"`
	Actions []MenuBarAction `json:"actions"`
}

// MenuBarAction is an action a menu click can run with no further input.
// Clipboard actions are included: they render text for the caller to copy
// rather than enqueueing a command.
type MenuBarAction struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Clipboard bool   `json:"clipboard"`
}

func (s *MenuBarService) Pins(context.Context) ([]MenuBarPin, error) {
	cfg, err := s.settings.Effective()
	if err != nil {
		return nil, Wrap(err, KindInternal, "reading settings")
	}
	pins := make([]MenuBarPin, 0, len(cfg.MenuBar.Feeds))
	for _, feed := range cfg.MenuBar.Feeds {
		pins = append(pins, MenuBarPin{Feed: feed.Feed, Limit: feed.ItemLimit()})
	}
	return pins, nil
}

// SetPins replaces the pinned feeds, top first. Pins are not checked against
// the loaded flows, for the same reason profiles.order is not: a pin naming
// nothing is skipped when read.
func (s *MenuBarService) SetPins(ctx context.Context, pins []MenuBarPin) error {
	next := settings.MenuBarSettings{Feeds: make([]settings.MenuBarFeed, 0, len(pins))}
	for _, pin := range pins {
		limit := pin.Limit
		if limit == settings.DefaultMenuBarItemLimit {
			limit = 0
		}
		next.Feeds = append(next.Feeds, settings.MenuBarFeed{Feed: pin.Feed, Limit: limit})
	}
	if err := next.Validate(); err != nil {
		return Wrap(err, KindInvalid, "pinning menu bar feeds")
	}
	if _, err := s.settings.Update(func(current *settings.Settings) error {
		current.MenuBar = next
		return nil
	}); err != nil {
		return Wrap(err, KindInternal, "saving the menu bar feeds")
	}
	s.events.Publish(ctx, events.MenuBarUpdated{})
	return nil
}

// FeedChoices lists every feed of every loaded profile, in rail then
// declaration order.
func (s *MenuBarService) FeedChoices(context.Context) []MenuBarFeedChoice {
	choices := make([]MenuBarFeedChoice, 0)
	for _, f := range s.flows.List() {
		folders := s.feedFolders(f.ID)
		for _, node := range f.FeedNodes() {
			choices = append(choices, MenuBarFeedChoice{Feed: f.FeedID(node.ID), MenuBarFeedName: feedName(f, node, folders)})
		}
	}
	return choices
}

func (s *MenuBarService) Snapshot(ctx context.Context) (MenuBarSnapshot, error) {
	cfg, err := s.settings.Effective()
	if err != nil {
		return MenuBarSnapshot{}, Wrap(err, KindInternal, "reading settings")
	}
	counts := feedCountCache{items: s.items, byProfile: map[string]map[string]stores.FeedCount{}}
	runnable := s.runnableActions()

	snapshot := MenuBarSnapshot{Pinned: []MenuBarFeedView{}, LastPolled: s.LastPolled()}
	for _, pin := range cfg.MenuBar.Feeds {
		profileID, nodeID, _ := strings.Cut(pin.Feed, "/")
		f, ok := s.flows.Get(profileID)
		if !ok {
			continue
		}
		node, ok := feedNode(f, nodeID)
		if !ok {
			continue
		}
		count, err := counts.get(ctx, profileID, pin.Feed)
		if err != nil {
			return MenuBarSnapshot{}, err
		}
		rows, err := s.items.ListByFeed(ctx, profileID, pin.Feed, pin.ItemLimit())
		if err != nil {
			return MenuBarSnapshot{}, Wrap(err, KindInternal, "listing feed %q", pin.Feed)
		}
		view := MenuBarFeedView{
			ProfileID: profileID, Feed: pin.Feed, MenuBarFeedName: feedName(f, node, s.feedFolders(profileID)), Unread: count.Unread,
			Total: count.Total,
			Items: make([]MenuBarItem, 0, len(rows)),
		}
		for _, row := range rows {
			view.Items = append(view.Items, menuBarItem(row, runnable))
		}
		snapshot.Pinned = append(snapshot.Pinned, view)
	}

	return snapshot, nil
}

// LastPolled is when sources were last polled; zero before the first poll
// and in mock modes.
func (s *MenuBarService) LastPolled() time.Time {
	if s.polls == nil {
		return time.Time{}
	}
	return s.polls.LastTick()
}

// runnableActions is the catalog subset a menu click can run: offered on
// items and needing no input. An action that needs a form or the New Session
// dialog stays in the main window.
func (s *MenuBarService) runnableActions() []actions.Action {
	out := make([]actions.Action, 0)
	for _, action := range s.actions.List() {
		if action.ShowInDetail && action.RunsWithoutInput() {
			out = append(out, action)
		}
	}
	return out
}

func menuBarItem(row stores.InboxItem, runnable []actions.Action) MenuBarItem {
	var fields struct {
		Repo   string `json:"repo"`
		Num    int    `json:"num"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(row.Payload, &fields)

	item := MenuBarItem{
		ID: row.ID, Title: row.Title, URL: row.URL, Unread: row.Unread,
		Repo: fields.Repo, Number: fields.Num, Reason: fields.Reason,
		Actions: []MenuBarAction{},
	}
	decoded, err := dispatch.DecodeActionItem(row.Payload, row.ExternalID)
	if err != nil {
		return item
	}
	item.Kind = decoded.Kind
	for _, action := range runnable {
		if ok, _ := dispatch.ActionApplicability(action, decoded); !ok {
			continue
		}
		_, clipboard := action.Config.(*actions.ClipboardConfig)
		item.Actions = append(item.Actions, MenuBarAction{ID: action.ID, Label: action.Label, Clipboard: clipboard})
	}
	return item
}

// feedFolders maps a profile's feed node ids to the sidebar folder holding
// them. The sidebar layout is cosmetic, so a missing file means no folders.
func (s *MenuBarService) feedFolders(profileID string) map[string]string {
	folders := map[string]string{}
	for _, item := range s.flows.GetSidebar(profileID).Items {
		if item.Folder == nil {
			continue
		}
		for _, nodeID := range item.Folder.Feeds {
			folders[nodeID] = item.Folder.Name
		}
	}
	return folders
}

func feedName(f flow.Flow, node flow.Node, folders map[string]string) MenuBarFeedName {
	return MenuBarFeedName{ProfileName: cmp.Or(f.Name, f.ID), Folder: folders[node.ID], Name: cmp.Or(node.Name, node.ID)}
}

func feedNode(f flow.Flow, nodeID string) (flow.Node, bool) {
	for _, node := range f.FeedNodes() {
		if node.ID == nodeID {
			return node, true
		}
	}
	return flow.Node{}, false
}

// feedCountCache reads each profile's counts once per snapshot.
type feedCountCache struct {
	items     *stores.InboxItemStore
	byProfile map[string]map[string]stores.FeedCount
}

func (c feedCountCache) get(ctx context.Context, profileID, feedID string) (stores.FeedCount, error) {
	byFeed, ok := c.byProfile[profileID]
	if !ok {
		counts, err := c.items.FeedCounts(ctx, profileID)
		if err != nil {
			return stores.FeedCount{}, Wrap(err, KindInternal, "counting feeds for %q", profileID)
		}
		byFeed = make(map[string]stores.FeedCount, len(counts))
		for _, count := range counts {
			byFeed[count.FeedID] = count
		}
		c.byProfile[profileID] = byFeed
	}
	return byFeed[feedID], nil
}
