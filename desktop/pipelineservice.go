package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// PipelineService is the Wails service exposing the desktop pipeline's
// event log, configured actions, and commit protocol to the frontend.
type PipelineService struct {
	db            *store.DB
	actions       *actions.ActionStore
	worker        *ingest.Worker
	launchOptions ingest.SessionLaunchOptionsProvider
}

func NewPipelineService(db *store.DB, actionStore *actions.ActionStore, worker *ingest.Worker, launchOptions ingest.SessionLaunchOptionsProvider) *PipelineService {
	return &PipelineService{db: db, actions: actionStore, worker: worker, launchOptions: launchOptions}
}

// ReadFrom returns up to limit event_log rows after consumer's persisted
// offset, in ascending order. The frontend never supplies an offset: the
// SQLite checkpoint is the source of truth across runtime restarts.
func (s *PipelineService) ReadFrom(consumer string, limit int) ([]store.Msg, error) {
	return s.db.ReadForConsumer(context.Background(), consumer, limit)
}

// Commit applies the frontend graph runtime's batch atomically. Feed outputs
// are accepted without durable effect until membership claims land; action
// outputs, node-run metrics, and the consumer offset are persisted atomically.
// Idempotent by offset: replaying a batch already applied (UpToOffset <= the
// consumer's current offset) is a no-op.
func (s *PipelineService) Commit(batch store.CommitBatch) error {
	return s.db.CommitBatch(context.Background(), batch)
}

// EventLogTailOffset returns a Wails-safe decimal tail for the startup/deploy
// replay protocol.
func (s *PipelineService) EventLogTailOffset() (string, error) {
	tail, err := s.db.EventLogTailOffset(context.Background())
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(tail, 10), nil
}

// ActivateReplay atomically advances the consumer and installs the prepared
// membership state for a startup or deploy replay.
func (s *PipelineService) ActivateReplay(profileID, tail string, claims []store.FeedMembershipClaim, feedIDs, sourceIDs []string) error {
	offset, err := strconv.ParseInt(tail, 10, 64)
	if err != nil || offset < 0 {
		return fmt.Errorf("invalid event log tail %q", tail)
	}
	return s.db.ActivateReplay(context.Background(), profileID, offset, claims, feedIDs, sourceIDs)
}

// ListUnarchivedInboxItems returns the JSON/Wails-friendly immutable inbox
// identity and payload needed for claims-only synthetic replay.
func (s *PipelineService) ListUnarchivedInboxItems(profileID string) ([]store.InboxItemView, error) {
	return s.db.ListUnarchivedInboxItems(context.Background(), profileID)
}

// ListReplaySourceSnapshots returns each source's latest authoritative
// snapshot so membership replay preserves source provenance.
func (s *PipelineService) ListReplaySourceSnapshots(profileID, throughOffset string) ([]store.Msg, error) {
	offset, err := strconv.ParseInt(throughOffset, 10, 64)
	if err != nil || offset < 0 {
		return nil, fmt.Errorf("invalid replay snapshot offset %q", throughOffset)
	}
	return s.db.ListReplaySourceSnapshots(context.Background(), profileID, offset)
}

func (s *PipelineService) ListInboxItemsByFeed(profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.db.ListInboxItemsByFeed(context.Background(), profileID, feedID, limit)
}

// ListArchivedInboxItemsByFeed returns a feed's archived section, loaded
// lazily when the user expands the archived divider.
func (s *PipelineService) ListArchivedInboxItemsByFeed(profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.db.ListArchivedInboxItemsByFeed(context.Background(), profileID, feedID, limit)
}

// ListInboxItemsTrash returns unrouted and ignored items for the Trash
// utility view.
func (s *PipelineService) ListInboxItemsTrash(profileID string, limit int) ([]store.InboxItemView, error) {
	return s.db.ListInboxItemsTrash(context.Background(), profileID, limit)
}

// InboxItemFeed returns the feed that holds an item, or "" when no feed
// claims it (an unrouted item, shown in Trash). The frontend uses it to turn
// a clicked notification into a feed route that reveals the item.
func (s *PipelineService) InboxItemFeed(profileID string, itemID int64) (string, error) {
	return s.db.InboxItemFeedID(context.Background(), profileID, itemID)
}

func (s *PipelineService) InboxItemEvents(itemID int64, limit int) ([]store.InboxEventView, error) {
	return s.db.InboxItemEvents(context.Background(), itemID, limit)
}

func (s *PipelineService) MarkInboxItemUnread(itemID, revision int64, unread bool) (store.InboxItemView, error) {
	return s.db.SetInboxItemUnread(context.Background(), itemID, revision, unread)
}

// MarkInboxItemsRead clears unread for a whole scope in one write: the named
// feed, or every feed in the workspace when feedID is empty. Archived and
// ignored items keep their state. It returns the number of items cleared,
// which is what the frontend reports back to the user.
func (s *PipelineService) MarkInboxItemsRead(profileID, feedID string) (int64, error) {
	return s.db.MarkInboxItemsRead(context.Background(), profileID, feedID)
}

func (s *PipelineService) ToggleInboxItemArchived(itemID, revision int64) (store.InboxItemView, error) {
	return s.db.ToggleInboxItemArchived(context.Background(), itemID, revision, time.Now().UnixMilli())
}

func (s *PipelineService) ToggleInboxItemIgnored(itemID, revision int64) (store.InboxItemView, error) {
	return s.db.ToggleInboxItemIgnored(context.Background(), itemID, revision, time.Now().UnixMilli())
}

func (s *PipelineService) FeedCounts(profileID string) ([]store.FeedInboxCount, error) {
	return s.db.FeedCounts(context.Background(), profileID)
}

// ActionViews returns the configured actions applicable to an inbox item:
// shown-in-detail, applies_to matches the item's canonical kind, and every
// hard template capability is satisfiable for the item's payload.
func (s *PipelineService) ActionViews(itemID int64) ([]actions.View, error) {
	row, err := s.db.Queries().GetInboxItemByID(context.Background(), itemID)
	if err != nil {
		return nil, fmt.Errorf("reading inbox item %d: %w", itemID, err)
	}
	item, err := ingest.DecodeActionItem(row.Payload, row.ExternalID)
	if err != nil {
		return nil, fmt.Errorf("decode inbox item %d payload: %w", itemID, err)
	}
	views := make([]actions.View, 0)
	for _, action := range s.actions.List() {
		if !action.ShowInDetail {
			continue
		}
		if ok, _ := ingest.ActionApplicability(action, item); !ok {
			continue
		}
		views = append(views, action.View())
	}
	return views, nil
}

// SessionLaunchOptions supplies the configured repository and agent choices
// for interactive launch-session actions. It intentionally exposes no local
// checkout paths or executable action configuration.
func (s *PipelineService) SessionLaunchOptions() (ingest.SessionLaunchOptions, error) {
	if s.launchOptions == nil {
		return ingest.SessionLaunchOptions{}, fmt.Errorf("session launch options are unavailable")
	}
	return s.launchOptions.SessionLaunchOptions(context.Background())
}

// InvokeAction records the user's explicit confirmation for actionID against
// item and executes it. It accepts only actions that apply to the item's kind;
// executable configuration is always re-resolved from ActionStore.
func (s *PipelineService) InvokeAction(actionID string, itemID int64, input ingest.ActionInvocationInput) (ingest.ActionRunView, error) {
	row, err := s.db.Queries().GetInboxItemByID(context.Background(), itemID)
	if err != nil {
		return ingest.ActionRunView{}, fmt.Errorf("reading inbox item %d: %w", itemID, err)
	}
	item, err := ingest.DecodeActionItem(row.Payload, row.ExternalID)
	if err != nil {
		return ingest.ActionRunView{}, fmt.Errorf("decode inbox item %d payload: %w", itemID, err)
	}
	action, ok := s.actions.Get(actionID)
	if !ok {
		return ingest.ActionRunView{}, fmt.Errorf("unknown action %q", actionID)
	}
	if !action.ShowInDetail {
		return ingest.ActionRunView{}, fmt.Errorf("action %q is not available in the detail pane", actionID)
	}
	if applicable, reason := ingest.ActionApplicability(action, item); !applicable {
		return ingest.ActionRunView{}, fmt.Errorf("action %q does not apply to item %d: %s", actionID, itemID, reason)
	}
	if item.ID == "" {
		return ingest.ActionRunView{}, fmt.Errorf("action %q: item id is required", actionID)
	}
	if s.worker == nil {
		return ingest.ActionRunView{}, fmt.Errorf("action execution is unavailable")
	}
	return s.worker.Confirm(context.Background(), actionID, item.ID, item.Payload, input)
}

// NodeRuns returns up to limit of a flow's most recent node_run rows,
// newest first, for the flows canvas's live per-node status and RECENT
// activity list.
func (s *PipelineService) NodeRuns(flowID string, limit int) ([]store.NodeRunRecord, error) {
	return s.db.NodeRuns(context.Background(), flowID, limit)
}

func (s *PipelineService) ActionRun(commandID int64) (ingest.ActionRunView, error) {
	row, err := s.db.OutputCommand(context.Background(), commandID)
	if err != nil {
		return ingest.ActionRunView{}, err
	}
	view := ingest.ActionRunView{CommandID: row.ID, Status: row.Status}
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
			return ingest.ActionRunView{}, fmt.Errorf("decode action run %d result: %w", commandID, err)
		}
	}
	return view, nil
}
