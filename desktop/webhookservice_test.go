package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/pipelinedb"
)

func TestWebhookServiceInfoWithoutListener(t *testing.T) {
	service := NewWebhookService(nil, nil, 4483)
	info := service.Info()
	assert.False(t, info.Running)
	assert.Equal(t, 4483, info.Port)
	assert.Equal(t, "http://127.0.0.1:4483/hooks/", info.BaseURL)
}

func TestWebhookServiceCapture(t *testing.T) {
	db, err := pipelinedb.Open(t.TempDir(), pipelinedb.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	service := NewWebhookService(db, nil, 4483)

	// No delivery captured yet: zero view, no error.
	view, err := service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.Zero(t, view.ReceivedAt)

	ctx := context.Background()
	require.NoError(t, db.Queries().UpsertWebhookCapture(ctx, pipelinedb.UpsertWebhookCaptureParams{
		Topic: "source:triage/hook", ReceivedAt: 42, Body: []byte(`{"event":"deploy"}`),
	}))
	view, err = service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.Equal(t, int64(42), view.ReceivedAt)
	assert.JSONEq(t, `{"event":"deploy"}`, view.Body)
	assert.False(t, view.FeedShaped)
	assert.Equal(t, []string{"id", "kind", "repo", "title", "url"}, view.MissingFields)

	require.NoError(t, db.Queries().UpsertWebhookCapture(ctx, pipelinedb.UpsertWebhookCaptureParams{
		Topic: "source:triage/hook", ReceivedAt: 43,
		Body: []byte(`{"id":"1","kind":"Alert","repo":"o/r","title":"t","url":"https://x"}`),
	}))
	view, err = service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.True(t, view.FeedShaped)
	assert.Empty(t, view.MissingFields)
}
