package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

// PipelineService exposes the inbox to the frontend. Every method is a
// request build plus one call into the core.
//
// The event log is not on this surface. Reading it, committing runs against
// it, and the replay protocol used to be RPCs because the graph ran in the
// browser; with the engine in Go they are internal calls, and the
// decimal-string offset encoding they needed to survive the JavaScript number
// boundary went with them.
type PipelineService struct {
	inbox *app.InboxService
}

func NewPipelineService(inbox *app.InboxService) *PipelineService {
	return &PipelineService{inbox: inbox}
}

func (s *PipelineService) ListByFeed(ctx context.Context, profileID, feedID string, limit int) ([]stores.InboxItem, error) {
	return s.inbox.ListByFeed(ctx, profileID, feedID, limit)
}

func (s *PipelineService) ListArchivedByFeed(ctx context.Context, profileID, feedID string, limit int) ([]stores.InboxItem, error) {
	return s.inbox.ListArchivedByFeed(ctx, profileID, feedID, limit)
}

func (s *PipelineService) ListTrash(ctx context.Context, profileID string, limit int) ([]stores.InboxItem, error) {
	return s.inbox.ListTrash(ctx, profileID, limit)
}

func (s *PipelineService) Feed(ctx context.Context, profileID string, itemID int64) (string, error) {
	return s.inbox.Feed(ctx, profileID, itemID)
}

func (s *PipelineService) Events(ctx context.Context, itemID int64, limit int) ([]stores.InboxEvent, error) {
	return s.inbox.Events(ctx, itemID, limit)
}

func (s *PipelineService) SetUnread(ctx context.Context, itemID, revision int64, unread bool) (stores.InboxItem, error) {
	return s.inbox.SetUnread(ctx, itemID, revision, unread)
}

func (s *PipelineService) MarkRead(ctx context.Context, profileID, feedID string) (int64, error) {
	return s.inbox.MarkRead(ctx, profileID, feedID)
}

func (s *PipelineService) ToggleArchived(ctx context.Context, itemID, revision int64) (stores.InboxItem, error) {
	return s.inbox.ToggleArchived(ctx, itemID, revision)
}

func (s *PipelineService) ToggleIgnored(ctx context.Context, itemID, revision int64) (stores.InboxItem, error) {
	return s.inbox.ToggleIgnored(ctx, itemID, revision)
}

func (s *PipelineService) FeedCounts(ctx context.Context, profileID string) ([]stores.FeedCount, error) {
	return s.inbox.FeedCounts(ctx, profileID)
}

func (s *PipelineService) ActionViews(ctx context.Context, itemID int64) ([]actions.View, error) {
	return s.inbox.ActionViews(ctx, itemID)
}

func (s *PipelineService) NewSessionDraft(ctx context.Context, itemID int64) (dispatch.SessionDraft, error) {
	return s.inbox.NewSessionDraft(ctx, itemID)
}

func (s *PipelineService) InvokeAction(ctx context.Context, actionID string, itemID int64, input dispatch.ActionInvocationInput) (dispatch.ActionRunView, error) {
	return s.inbox.InvokeAction(ctx, app.InvokeActionRequest{
		ActionID: actionID,
		ItemID:   itemID,
		Input:    input,
	})
}

// RenderClipboardAction returns the text a clipboard action renders for an
// item. The frontend writes it to the clipboard through the native Wails
// clipboard; the core produces the text and never touches the clipboard.
func (s *PipelineService) RenderClipboardAction(ctx context.Context, actionID string, itemID int64, inputs map[string]string) (string, error) {
	return s.inbox.RenderClipboardAction(ctx, actionID, itemID, inputs)
}

func (s *PipelineService) NodeRuns(ctx context.Context, flowID string, limit int) ([]stores.NodeRunRecord, error) {
	return s.inbox.NodeRuns(ctx, flowID, limit)
}

func (s *PipelineService) ActionRun(ctx context.Context, commandID int64) (dispatch.ActionRunView, error) {
	return s.inbox.ActionRun(ctx, commandID)
}
