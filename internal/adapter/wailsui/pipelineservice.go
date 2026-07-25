package wailsui

import (
	"context"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// PipelineService exposes the inbox to the frontend. Every method is a
// request build plus one call into the core; the int64-as-string encodings
// below are the transport precision workaround and stay on this side.
type PipelineService struct {
	inbox *app.InboxService
}

func NewPipelineService(inbox *app.InboxService) *PipelineService {
	return &PipelineService{inbox: inbox}
}

func (s *PipelineService) ReadFrom(consumer string, limit int) ([]store.Msg, error) {
	return s.inbox.ReadFrom(context.Background(), consumer, limit)
}

func (s *PipelineService) Commit(batch store.CommitBatch) error {
	return s.inbox.Commit(context.Background(), batch)
}

// EventLogTailOffset returns a Wails-safe decimal tail for the startup/deploy
// replay protocol. The core deals in int64.
func (s *PipelineService) EventLogTailOffset() (string, error) {
	tail, err := s.inbox.EventLogTailOffset(context.Background())
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(tail, 10), nil
}

func (s *PipelineService) ActivateReplay(profileID, tail string, claims []store.FeedMembershipClaim, feedIDs, sourceIDs []string) error {
	offset, err := parseOffset(tail, "event log tail")
	if err != nil {
		return err
	}
	return s.inbox.ActivateReplay(context.Background(), app.ActivateReplayRequest{
		ProfileID: profileID,
		Tail:      offset,
		Claims:    claims,
		FeedIDs:   feedIDs,
		SourceIDs: sourceIDs,
	})
}

func (s *PipelineService) ListUnarchivedInboxItems(profileID string) ([]store.InboxItemView, error) {
	return s.inbox.ListUnarchivedInboxItems(context.Background(), profileID)
}

func (s *PipelineService) ListReplaySourceSnapshots(profileID, throughOffset string) ([]store.Msg, error) {
	offset, err := parseOffset(throughOffset, "replay snapshot offset")
	if err != nil {
		return nil, err
	}
	return s.inbox.ListReplaySourceSnapshots(context.Background(), profileID, offset)
}

func (s *PipelineService) ListInboxItemsByFeed(profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListInboxItemsByFeed(context.Background(), profileID, feedID, limit)
}

func (s *PipelineService) ListArchivedInboxItemsByFeed(profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListArchivedInboxItemsByFeed(context.Background(), profileID, feedID, limit)
}

func (s *PipelineService) ListInboxItemsTrash(profileID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListInboxItemsTrash(context.Background(), profileID, limit)
}

func (s *PipelineService) InboxItemFeed(profileID string, itemID int64) (string, error) {
	return s.inbox.InboxItemFeed(context.Background(), profileID, itemID)
}

func (s *PipelineService) InboxItemEvents(itemID int64, limit int) ([]store.InboxEventView, error) {
	return s.inbox.InboxItemEvents(context.Background(), itemID, limit)
}

func (s *PipelineService) MarkInboxItemUnread(itemID, revision int64, unread bool) (store.InboxItemView, error) {
	return s.inbox.MarkInboxItemUnread(context.Background(), itemID, revision, unread)
}

func (s *PipelineService) MarkInboxItemsRead(profileID, feedID string) (int64, error) {
	return s.inbox.MarkInboxItemsRead(context.Background(), profileID, feedID)
}

func (s *PipelineService) ToggleInboxItemArchived(itemID, revision int64) (store.InboxItemView, error) {
	return s.inbox.ToggleInboxItemArchived(context.Background(), itemID, revision)
}

func (s *PipelineService) ToggleInboxItemIgnored(itemID, revision int64) (store.InboxItemView, error) {
	return s.inbox.ToggleInboxItemIgnored(context.Background(), itemID, revision)
}

func (s *PipelineService) FeedCounts(profileID string) ([]store.FeedInboxCount, error) {
	return s.inbox.FeedCounts(context.Background(), profileID)
}

func (s *PipelineService) ActionViews(itemID int64) ([]actions.View, error) {
	return s.inbox.ActionViews(context.Background(), itemID)
}

func (s *PipelineService) SessionLaunchOptions() (dispatch.SessionLaunchOptions, error) {
	return s.inbox.SessionLaunchOptions(context.Background())
}

func (s *PipelineService) InvokeAction(actionID string, itemID int64, input dispatch.ActionInvocationInput) (dispatch.ActionRunView, error) {
	return s.inbox.InvokeAction(context.Background(), app.InvokeActionRequest{
		ActionID: actionID,
		ItemID:   itemID,
		Input:    input,
	})
}

func (s *PipelineService) NodeRuns(flowID string, limit int) ([]store.NodeRunRecord, error) {
	return s.inbox.NodeRuns(context.Background(), flowID, limit)
}

func (s *PipelineService) ActionRun(commandID int64) (dispatch.ActionRunView, error) {
	return s.inbox.ActionRun(context.Background(), commandID)
}

// parseOffset decodes one of the decimal-string offsets the binding carries.
// It is the only place the encoding is undone, and a malformed value is the
// caller's mistake rather than ours.
func parseOffset(raw, what string) (int64, error) {
	offset, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || offset < 0 {
		return 0, app.Errorf(app.KindInvalid, "invalid %s %q", what, raw)
	}
	return offset, nil
}
