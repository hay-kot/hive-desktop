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

func (s *PipelineService) ReadFrom(ctx context.Context, consumer string, limit int) ([]store.Msg, error) {
	return s.inbox.ReadFrom(ctx, consumer, limit)
}

func (s *PipelineService) Commit(ctx context.Context, batch store.CommitBatch) error {
	return s.inbox.Commit(ctx, batch)
}

// EventLogTailOffset returns a Wails-safe decimal tail for the startup/deploy
// replay protocol. The core deals in int64.
func (s *PipelineService) EventLogTailOffset(ctx context.Context) (string, error) {
	tail, err := s.inbox.EventLogTailOffset(ctx)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(tail, 10), nil
}

func (s *PipelineService) ActivateReplay(ctx context.Context, profileID, tail string, claims []store.FeedMembershipClaim, feedIDs, sourceIDs []string) error {
	offset, err := parseOffset(tail, "event log tail")
	if err != nil {
		return err
	}
	return s.inbox.ActivateReplay(ctx, app.ActivateReplayRequest{
		ProfileID: profileID,
		Tail:      offset,
		Claims:    claims,
		FeedIDs:   feedIDs,
		SourceIDs: sourceIDs,
	})
}

func (s *PipelineService) ListUnarchivedInboxItems(ctx context.Context, profileID string) ([]store.InboxItemView, error) {
	return s.inbox.ListUnarchivedInboxItems(ctx, profileID)
}

func (s *PipelineService) ListReplaySourceSnapshots(ctx context.Context, profileID, throughOffset string) ([]store.Msg, error) {
	offset, err := parseOffset(throughOffset, "replay snapshot offset")
	if err != nil {
		return nil, err
	}
	return s.inbox.ListReplaySourceSnapshots(ctx, profileID, offset)
}

func (s *PipelineService) ListInboxItemsByFeed(ctx context.Context, profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListInboxItemsByFeed(ctx, profileID, feedID, limit)
}

func (s *PipelineService) ListArchivedInboxItemsByFeed(ctx context.Context, profileID, feedID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListArchivedInboxItemsByFeed(ctx, profileID, feedID, limit)
}

func (s *PipelineService) ListInboxItemsTrash(ctx context.Context, profileID string, limit int) ([]store.InboxItemView, error) {
	return s.inbox.ListInboxItemsTrash(ctx, profileID, limit)
}

func (s *PipelineService) InboxItemFeed(ctx context.Context, profileID string, itemID int64) (string, error) {
	return s.inbox.InboxItemFeed(ctx, profileID, itemID)
}

func (s *PipelineService) InboxItemEvents(ctx context.Context, itemID int64, limit int) ([]store.InboxEventView, error) {
	return s.inbox.InboxItemEvents(ctx, itemID, limit)
}

func (s *PipelineService) MarkInboxItemUnread(ctx context.Context, itemID, revision int64, unread bool) (store.InboxItemView, error) {
	return s.inbox.MarkInboxItemUnread(ctx, itemID, revision, unread)
}

func (s *PipelineService) MarkInboxItemsRead(ctx context.Context, profileID, feedID string) (int64, error) {
	return s.inbox.MarkInboxItemsRead(ctx, profileID, feedID)
}

func (s *PipelineService) ToggleInboxItemArchived(ctx context.Context, itemID, revision int64) (store.InboxItemView, error) {
	return s.inbox.ToggleInboxItemArchived(ctx, itemID, revision)
}

func (s *PipelineService) ToggleInboxItemIgnored(ctx context.Context, itemID, revision int64) (store.InboxItemView, error) {
	return s.inbox.ToggleInboxItemIgnored(ctx, itemID, revision)
}

func (s *PipelineService) FeedCounts(ctx context.Context, profileID string) ([]store.FeedInboxCount, error) {
	return s.inbox.FeedCounts(ctx, profileID)
}

func (s *PipelineService) ActionViews(ctx context.Context, itemID int64) ([]actions.View, error) {
	return s.inbox.ActionViews(ctx, itemID)
}

func (s *PipelineService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	return s.inbox.SessionLaunchOptions(ctx)
}

func (s *PipelineService) InvokeAction(ctx context.Context, actionID string, itemID int64, input dispatch.ActionInvocationInput) (dispatch.ActionRunView, error) {
	return s.inbox.InvokeAction(ctx, app.InvokeActionRequest{
		ActionID: actionID,
		ItemID:   itemID,
		Input:    input,
	})
}

func (s *PipelineService) NodeRuns(ctx context.Context, flowID string, limit int) ([]store.NodeRunRecord, error) {
	return s.inbox.NodeRuns(ctx, flowID, limit)
}

func (s *PipelineService) ActionRun(ctx context.Context, commandID int64) (dispatch.ActionRunView, error) {
	return s.inbox.ActionRun(ctx, commandID)
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
