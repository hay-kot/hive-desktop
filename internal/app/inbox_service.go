package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// InboxService owns the durable inbox as a *reader and triager* sees it: the
// item views the sidebar renders and the explicit action invocations a user
// confirms from the detail pane.
//
// The event log itself is deliberately not here. Reading it, committing runs
// against it, and the replay protocol are the flow engine's, and the engine
// talks to the store directly — routing them through a service would only put
// a facade between two parts of the core with no caller in between.
type InboxService struct {
	db      *store.DB
	actions *actions.ActionStore
	worker  *dispatch.Worker
	launch  dispatch.SessionLaunchOptionsProvider
}

func newInboxService(db *store.DB, catalog *actions.ActionStore, worker *dispatch.Worker, launch dispatch.SessionLaunchOptionsProvider) *InboxService {
	return &InboxService{db: db, actions: catalog, worker: worker, launch: launch}
}

func (s *InboxService) ListInboxItemsByFeed(ctx context.Context, profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	items, err := s.db.ListInboxItemsByFeed(ctx, profileID, feedID, limit)
	return items, Wrap(err, KindInternal, "listing feed %q", feedID)
}

// ListArchivedInboxItemsByFeed returns a feed's archived section, loaded
// lazily when the user expands the archived divider.
func (s *InboxService) ListArchivedInboxItemsByFeed(ctx context.Context, profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	items, err := s.db.ListArchivedInboxItemsByFeed(ctx, profileID, feedID, limit)
	return items, Wrap(err, KindInternal, "listing archived items in feed %q", feedID)
}

// ListInboxItemsTrash returns unrouted and ignored items for the Trash view.
func (s *InboxService) ListInboxItemsTrash(ctx context.Context, profileID string, limit int) ([]store.InboxItemView, error) {
	items, err := s.db.ListInboxItemsTrash(ctx, profileID, limit)
	return items, Wrap(err, KindInternal, "listing trash for %q", profileID)
}

// InboxItemFeed returns the feed that holds an item, or "" when no feed
// claims it (an unrouted item, shown in Trash). It is what turns a clicked
// notification into a route that reveals the item.
func (s *InboxService) InboxItemFeed(ctx context.Context, profileID string, itemID int64) (string, error) {
	feedID, err := s.db.InboxItemFeedID(ctx, profileID, itemID)
	return feedID, Wrap(err, KindInternal, "resolving the feed for item %d", itemID)
}

// InboxItemFeeds resolves the claiming feed of each item in one query, for
// callers that list items flat (the agent API) and need each item's feed.
func (s *InboxService) InboxItemFeeds(ctx context.Context, itemIDs []int64) (map[int64]string, error) {
	feeds, err := s.db.InboxItemFeedIDs(ctx, itemIDs)
	return feeds, Wrap(err, KindInternal, "resolving feeds for %d items", len(itemIDs))
}

func (s *InboxService) InboxItemEvents(ctx context.Context, itemID int64, limit int) ([]store.InboxEventView, error) {
	views, err := s.db.InboxItemEvents(ctx, itemID, limit)
	return views, Wrap(err, KindInternal, "listing events for item %d", itemID)
}

func (s *InboxService) MarkInboxItemUnread(ctx context.Context, itemID, revision int64, unread bool) (store.InboxItemView, error) {
	view, err := s.db.SetInboxItemUnread(ctx, itemID, revision, unread)
	return view, s.itemWriteError(err, itemID)
}

// MarkInboxItemsRead clears unread for a whole scope in one write: the named
// feed, or every feed in the workspace when feedID is empty. Archived and
// ignored items keep their state. It returns how many items were cleared.
func (s *InboxService) MarkInboxItemsRead(ctx context.Context, profileID, feedID string) (int64, error) {
	n, err := s.db.MarkInboxItemsRead(ctx, profileID, feedID)
	return n, Wrap(err, KindInternal, "marking items read in %q", profileID)
}

func (s *InboxService) ToggleInboxItemArchived(ctx context.Context, itemID, revision int64) (store.InboxItemView, error) {
	view, err := s.db.ToggleInboxItemArchived(ctx, itemID, revision, time.Now().UnixMilli())
	return view, s.itemWriteError(err, itemID)
}

func (s *InboxService) ToggleInboxItemIgnored(ctx context.Context, itemID, revision int64) (store.InboxItemView, error) {
	view, err := s.db.ToggleInboxItemIgnored(ctx, itemID, revision, time.Now().UnixMilli())
	return view, s.itemWriteError(err, itemID)
}

// itemWriteError classifies a revision-guarded write. A stale revision is the
// caller's view having moved, which is the whole reason KindConflict exists:
// re-read and retry rather than report a failure.
func (s *InboxService) itemWriteError(err error, itemID int64) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrStaleInboxItem):
		return Wrap(err, KindConflict, "inbox item %d changed underneath you", itemID)
	default:
		return Wrap(err, KindInternal, "updating inbox item %d", itemID)
	}
}

func (s *InboxService) FeedCounts(ctx context.Context, profileID string) ([]store.FeedInboxCount, error) {
	counts, err := s.db.FeedCounts(ctx, profileID)
	return counts, Wrap(err, KindInternal, "counting feeds for %q", profileID)
}

// ListItems returns inbox items newest-first, optionally scoped to a profile
// (empty matches all), unfiltered by feed or triage — a debug/observation read.
func (s *InboxService) ListItems(ctx context.Context, profileID string, limit int) ([]store.InboxItemView, error) {
	items, err := s.db.ListAllInboxItems(ctx, profileID, limit)
	return items, Wrap(err, KindInternal, "listing inbox items")
}

// FindItems returns every inbox item sharing an external id, optionally scoped
// to a profile (empty profileID matches all). The same external id can exist
// across profiles and source scopes, hence a slice.
func (s *InboxService) FindItems(ctx context.Context, profileID, externalID string) ([]store.InboxItemView, error) {
	if externalID == "" {
		return nil, Errorf(KindInvalid, "external id is required")
	}
	items, err := s.db.FindInboxItemsByExternalID(ctx, profileID, externalID)
	return items, Wrap(err, KindInternal, "finding items for external id %q", externalID)
}

// ActionViews returns the configured actions applicable to an inbox item:
// shown-in-detail, applies_to matches the item's canonical kind, and every
// hard template capability is satisfiable for the item's payload.
func (s *InboxService) ActionViews(ctx context.Context, itemID int64) ([]actions.View, error) {
	item, err := s.decodeItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	views := make([]actions.View, 0)
	for _, action := range s.actions.List() {
		if !action.ShowInDetail {
			continue
		}
		if ok, _ := dispatch.ActionApplicability(action, item); !ok {
			continue
		}
		views = append(views, action.View())
	}
	return views, nil
}

// SessionLaunchOptions supplies the configured repository and agent choices
// for interactive launch-session actions. It intentionally exposes no local
// checkout paths or executable action configuration.
func (s *InboxService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	if s.launch == nil {
		return dispatch.SessionLaunchOptions{}, Errorf(KindUnavailable, "session launch options are unavailable")
	}
	opts, err := s.launch.SessionLaunchOptions(ctx)
	return opts, Wrap(err, KindInternal, "resolving session launch options")
}

// InvokeActionRequest is one explicit, user-confirmed action invocation.
type InvokeActionRequest struct {
	ActionID string
	ItemID   int64
	Input    dispatch.ActionInvocationInput
}

// InvokeAction records the user's explicit confirmation and executes.
//
// It carries the whole authorization chain: the item must decode, the action
// must exist in the catalog, be shown in the detail pane, apply to the item's
// kind, and the item must have an id. Executable configuration is always
// re-resolved from the catalog rather than taken from the caller.
func (s *InboxService) InvokeAction(ctx context.Context, req InvokeActionRequest) (dispatch.ActionRunView, error) {
	item, err := s.decodeItem(ctx, req.ItemID)
	if err != nil {
		return dispatch.ActionRunView{}, err
	}
	action, ok := s.actions.Get(req.ActionID)
	if !ok {
		return dispatch.ActionRunView{}, Errorf(KindNotFound, "unknown action %q", req.ActionID)
	}
	if !action.ShowInDetail {
		return dispatch.ActionRunView{}, Errorf(KindInvalid, "action %q is not available in the detail pane", req.ActionID)
	}
	if _, isClipboard := action.Config.(*actions.ClipboardConfig); isClipboard {
		// A clipboard action produces text, not a durable command. It must not
		// enqueue an output_command (RenderClipboardAction is its path), so the
		// worker never sees it and re-copying never prompts for a rerun.
		return dispatch.ActionRunView{}, Errorf(KindInvalid, "action %q is a clipboard action; copy it from the detail pane instead", req.ActionID)
	}
	if applicable, reason := dispatch.ActionApplicability(action, item); !applicable {
		return dispatch.ActionRunView{}, Errorf(KindInvalid, "action %q does not apply to item %d: %s", req.ActionID, req.ItemID, reason)
	}
	if item.ID == "" {
		return dispatch.ActionRunView{}, Errorf(KindInvalid, "action %q: item id is required", req.ActionID)
	}
	if s.worker == nil {
		return dispatch.ActionRunView{}, Errorf(KindUnavailable, "action execution is unavailable")
	}
	view, err := s.worker.Confirm(ctx, req.ActionID, item.ID, item.Payload, req.Input)
	if err != nil {
		return dispatch.ActionRunView{}, s.confirmError(err, req.ActionID)
	}
	return view, nil
}

// RenderClipboardAction resolves a clipboard action against an item and
// returns the rendered text for the desktop adapter to place on the clipboard.
//
// It is the render-only sibling of InvokeAction: a clipboard action produces
// text, not a durable side effect, so it never enqueues an output_command and
// re-copying the same item just renders again — there is no rerun to confirm.
// The authorization chain matches InvokeAction (the item must decode, the
// action must exist, be a clipboard action shown in the detail pane, apply to
// the item's kind, and the item must have an id); executable configuration is
// always re-resolved from the catalog rather than taken from the caller.
func (s *InboxService) RenderClipboardAction(ctx context.Context, actionID string, itemID int64) (string, error) {
	item, err := s.decodeItem(ctx, itemID)
	if err != nil {
		return "", err
	}
	action, ok := s.actions.Get(actionID)
	if !ok {
		return "", Errorf(KindNotFound, "unknown action %q", actionID)
	}
	if _, isClipboard := action.Config.(*actions.ClipboardConfig); !isClipboard {
		return "", Errorf(KindInvalid, "action %q is not a clipboard action", actionID)
	}
	if !action.ShowInDetail {
		return "", Errorf(KindInvalid, "action %q is not available in the detail pane", actionID)
	}
	if applicable, reason := dispatch.ActionApplicability(action, item); !applicable {
		return "", Errorf(KindInvalid, "action %q does not apply to item %d: %s", actionID, itemID, reason)
	}
	if item.ID == "" {
		return "", Errorf(KindInvalid, "action %q: item id is required", actionID)
	}
	text, err := dispatch.RenderClipboardText(action, item.ID, item.Payload)
	if err != nil {
		return "", Wrap(err, KindInvalid, "rendering clipboard action %q", actionID)
	}
	return text, nil
}

// confirmError classifies a refused confirmation. A rerun with no completed
// prior run is the caller asking for something that cannot exist yet, not a
// failure of ours — which is why the store had to start wrapping sql.ErrNoRows.
func (s *InboxService) confirmError(err error, actionID string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return Wrap(err, KindInvalid, "action %q has no completed run to repeat", actionID)
	}
	return Wrap(err, KindInternal, "invoking action %q", actionID)
}

// NodeRuns returns up to limit of a flow's most recent node_run rows, newest
// first, for the canvas's live per-node status and recent activity list.
func (s *InboxService) NodeRuns(ctx context.Context, flowID string, limit int) ([]store.NodeRunRecord, error) {
	runs, err := s.db.NodeRuns(ctx, flowID, limit)
	return runs, Wrap(err, KindInternal, "listing node runs for flow %q", flowID)
}

// ActionRun decodes one output_command row into a view. It owns the sql.Null*
// unwrapping and the result decode.
func (s *InboxService) ActionRun(ctx context.Context, commandID int64) (dispatch.ActionRunView, error) {
	row, err := s.db.OutputCommand(ctx, commandID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dispatch.ActionRunView{}, Wrap(err, KindNotFound, "action run %d not found", commandID)
		}
		return dispatch.ActionRunView{}, Wrap(err, KindInternal, "reading action run %d", commandID)
	}
	view := dispatch.ActionRunView{CommandID: row.ID, Status: row.Status}
	if row.LastError.Valid {
		view.Error = row.LastError.String
	}
	if row.Stdout.Valid {
		view.Stdout = row.Stdout.String
	}
	if row.Stderr.Valid {
		view.Stderr = row.Stderr.String
	}
	if row.ResultJson.Valid {
		if err := json.Unmarshal([]byte(row.ResultJson.String), &view.Result); err != nil {
			return dispatch.ActionRunView{}, Wrap(err, KindInternal, "decoding action run %d result", commandID)
		}
	}
	return view, nil
}

// decodeItem reads and decodes one inbox item, classifying the two ways it
// can fail: the row is gone, or its payload is not a canonical item.
func (s *InboxService) decodeItem(ctx context.Context, itemID int64) (dispatch.DecodedActionItem, error) {
	row, err := s.db.Queries().GetInboxItemByID(ctx, itemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dispatch.DecodedActionItem{}, Wrap(err, KindNotFound, "inbox item %d not found", itemID)
		}
		return dispatch.DecodedActionItem{}, Wrap(err, KindInternal, "reading inbox item %d", itemID)
	}
	item, err := dispatch.DecodeActionItem(row.Payload, row.ExternalID)
	if err != nil {
		return dispatch.DecodedActionItem{}, Wrap(err, KindInvalid, "decoding inbox item %d payload", itemID)
	}
	return item, nil
}
