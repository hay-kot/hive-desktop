package stores

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// WebhookCaptureStore owns webhook_capture: the last request body delivered
// to each webhook source node, kept for the flow editor's capture affordance.
type WebhookCaptureStore struct {
	q *queries.DB
}

func NewWebhookCaptureStore(q *queries.DB, _ Options) *WebhookCaptureStore {
	return &WebhookCaptureStore{q: q}
}

// Upsert records the latest delivery for topic.
func (s *WebhookCaptureStore) Upsert(ctx context.Context, topic string, receivedAt int64, body []byte) error {
	return wrap("upserting webhook capture", s.q.Ctx(ctx).UpsertWebhookCapture(ctx, queries.UpsertWebhookCaptureParams{
		Topic: topic, ReceivedAt: receivedAt, Body: body,
	}))
}

// Get reads the last delivery captured for topic.
func (s *WebhookCaptureStore) Get(ctx context.Context, topic string) (WebhookCapture, error) {
	row, err := s.q.Ctx(ctx).GetWebhookCapture(ctx, topic)
	if err != nil {
		return WebhookCapture{}, errTransformQueryOne("webhook_capture", topic, err)
	}
	return mapWebhookCaptureFromDB(row), nil
}
