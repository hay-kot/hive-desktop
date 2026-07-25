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

// InboxService owns the durable inbox: the event log a graph runtime reads
// and commits, the item views the sidebar renders, and the explicit action
// invocations a user confirms from the detail pane.
type InboxService struct {
	db      *store.DB
	actions *actions.ActionStore
	worker  *dispatch.Worker
	launch  dispatch.SessionLaunchOptionsProvider
}

func newInboxService(db *store.DB, catalog *actions.ActionStore, worker *dispatch.Worker, launch dispatch.SessionLaunchOptionsProvider) *InboxService {
	return &InboxService{db: db, actions: catalog, worker: worker, launch: launch}
}

// ReadFrom returns up to limit event_log rows after consumer's persisted
// offset, in ascending order. Callers never supply an offset: the SQLite
// checkpoint is the source of truth across restarts.
func (s *InboxService) ReadFrom(ctx context.Context, consumer string, limit int) ([]store.Msg, error) {
	msgs, err := s.db.ReadForConsumer(ctx, consumer, limit)
	return msgs, Wrap(err, KindInternal, "reading the event log for %q", consumer)
}

// Commit applies a graph runtime's batch atomically. Feed outputs are accepted
// without durable effect until membership claims land; action outputs,
// node-run metrics and the consumer offset are persisted together. Idempotent
// by offset: replaying a batch already applied is a no-op.
func (s *InboxService) Commit(ctx context.Context, batch store.CommitBatch) error {
	return Wrap(s.db.CommitBatch(ctx, batch), KindInternal, "committing batch for %q", batch.Consumer)
}

// EventLogTailOffset returns the tail as an int64. The decimal-string
// encoding the Wails binding needs is a transport concern and stays in the
// adapter.
func (s *InboxService) EventLogTailOffset(ctx context.Context) (int64, error) {
	tail, err := s.db.EventLogTailOffset(ctx)
	return tail, Wrap(err, KindInternal, "reading the event log tail")
}

// ActivateReplayRequest is the prepared membership state a startup or deploy
// replay installs.
type ActivateReplayRequest struct {
	ProfileID string
	Tail      int64
	Claims    []store.FeedMembershipClaim
	FeedIDs   []string
	SourceIDs []string
}

// ActivateReplay atomically advances the consumer and installs the prepared
// membership state.
func (s *InboxService) ActivateReplay(ctx context.Context, req ActivateReplayRequest) error {
	if req.Tail < 0 {
		return Errorf(KindInvalid, "event log tail must not be negative")
	}
	err := s.db.ActivateReplay(ctx, req.ProfileID, req.Tail, req.Claims, req.FeedIDs, req.SourceIDs)
	return Wrap(err, KindInternal, "activating replay for %q", req.ProfileID)
}

// ListUnarchivedInboxItems returns the immutable inbox identity and payload
// needed for claims-only synthetic replay.
func (s *InboxService) ListUnarchivedInboxItems(ctx context.Context, profileID string) ([]store.InboxItemView, error) {
	items, err := s.db.ListUnarchivedInboxItems(ctx, profileID)
	return items, Wrap(err, KindInternal, "listing inbox items for %q", profileID)
}

// ListReplaySourceSnapshots returns each source's latest authoritative
// snapshot so membership replay preserves source provenance.
func (s *InboxService) ListReplaySourceSnapshots(ctx context.Context, profileID string, throughOffset int64) ([]store.Msg, error) {
	if throughOffset < 0 {
		return nil, Errorf(KindInvalid, "replay snapshot offset must not be negative")
	}
	msgs, err := s.db.ListReplaySourceSnapshots(ctx, profileID, throughOffset)
	return msgs, Wrap(err, KindInternal, "listing replay snapshots for %q", profileID)
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
