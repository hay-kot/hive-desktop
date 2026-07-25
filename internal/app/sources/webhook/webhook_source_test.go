package webhook

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

func webhookFlow(flowID, nodeID, path, secret string) flow.Flow {
	return flow.Flow{
		ID:      flowID,
		Enabled: true,
		Nodes: []flow.Node{
			{ID: nodeID, Type: "webhook-source", Config: &flow.WebhookSourceConfig{Path: path, Secret: secret}},
		},
	}
}

type fakeFlows []flow.Flow

func (f fakeFlows) List() []flow.Flow { return f }

func newWebhookTestListener(t *testing.T, flows fakeFlows) (*Listener, *store.DB, *int64) {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var lastOffset int64
	listener := NewListener(db, flows, 0, func(offset int64) { lastOffset = offset }, zerolog.Nop())
	return listener, db, &lastOffset
}

func postHook(t *testing.T, handler http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestWebhookListenerIngestsDelivery(t *testing.T) {
	listener, db, lastOffset := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci-alerts", "")})
	handler := listener.Handler()

	body := `{"id":"build-42","title":"Build failed","url":"https://ci.example/42","status":"red"}`
	rec := postHook(t, handler, "/hooks/ci-alerts", body, nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"delivered":1}`, rec.Body.String())

	ctx := context.Background()
	item, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "build-42",
	})
	require.NoError(t, err)
	assert.Equal(t, "Build failed", item.Title)
	assert.Equal(t, "https://ci.example/42", item.Url)
	assert.JSONEq(t, body, string(item.Payload))

	// One routed observation row plus one authoritative snapshot row.
	msgs, err := db.ReadForConsumer(ctx, "triage", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "source:triage/hook", msgs[0].Topic)
	assert.Equal(t, "build-42", msgs[0].Key)
	require.Len(t, msgs[1].Snapshot, 1)
	assert.Equal(t, "build-42", msgs[1].Snapshot[0].Key)

	// The wake-up carries the snapshot row's offset (the last append).
	assert.Equal(t, msgs[1].ID, fmt.Sprint(*lastOffset))

	capture, err := db.Queries().GetWebhookCapture(ctx, "source:triage/hook")
	require.NoError(t, err)
	assert.JSONEq(t, body, string(capture.Body))
	assert.Positive(t, capture.ReceivedAt)
}

func TestWebhookListenerDeduplicatesUnchangedBody(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()
	body := `{"id":"x","title":"same"}`

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", body, nil).Code)
	first, err := db.Queries().GetWebhookCapture(context.Background(), "source:triage/hook")
	require.NoError(t, err)

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", body, nil).Code)

	ctx := context.Background()
	msgs, err := db.ReadForConsumer(ctx, "triage", 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 2, "an unchanged re-delivery must not append new event rows")

	// The capture still tracks the latest request.
	second, err := db.Queries().GetWebhookCapture(ctx, "source:triage/hook")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, second.ReceivedAt, first.ReceivedAt)
}

func TestWebhookListenerUpdatesChangedBodySameID(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","n":1}`, nil).Code)
	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","n":2}`, nil).Code)

	ctx := context.Background()
	item, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "x",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), item.Revision)
	assert.JSONEq(t, `{"id":"x","n":2}`, string(item.Payload))

	// Snapshot still contains exactly one item for the key.
	msgs, err := db.ReadForConsumer(ctx, "triage", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	assert.Len(t, msgs[3].Snapshot, 1)
}

func TestWebhookListenerContentHashKeyWithoutID(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"event":"a"}`, nil).Code)
	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"event":"b"}`, nil).Code)

	ctx := context.Background()
	rows, err := db.Queries().ListUnarchivedInboxItemsBySource(ctx, store.ListUnarchivedInboxItemsBySourceParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook",
	})
	require.NoError(t, err)
	require.Len(t, rows, 2, "distinct bodies without ids are distinct items")
	assert.Len(t, rows[0].ExternalID, 64, "content-hash key is the sha256 hex")
	assert.True(t, strings.HasPrefix(rows[0].Title, "ci · "), "fallback title includes the path: %q", rows[0].Title)
}

func TestWebhookListenerNumericID(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	require.Equal(t, http.StatusAccepted, postHook(t, listener.Handler(), "/hooks/ci", `{"id":1234}`, nil).Code)

	_, err := db.Queries().GetInboxItemByExternalID(context.Background(), store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "1234",
	})
	require.NoError(t, err)
}

func TestWebhookListenerSecret(t *testing.T) {
	listener, _, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "s3cret")})
	handler := listener.Handler()

	assert.Equal(t, http.StatusUnauthorized, postHook(t, handler, "/hooks/ci", `{}`, nil).Code)
	assert.Equal(t, http.StatusUnauthorized, postHook(t, handler, "/hooks/ci", `{}`, map[string]string{WebhookSecretHeader: "wrong"}).Code)
	assert.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{}`, map[string]string{WebhookSecretHeader: "s3cret"}).Code)
}

func TestWebhookListenerFansOutToMatchingNodes(t *testing.T) {
	flows := fakeFlows{
		webhookFlow("alpha", "hook-a", "shared", ""),
		webhookFlow("beta", "hook-b", "shared", "s3cret"),
	}
	listener, db, _ := newWebhookTestListener(t, flows)
	handler := listener.Handler()

	// Without the secret only alpha's node accepts; with it both do.
	rec := postHook(t, handler, "/hooks/shared", `{"id":"1"}`, nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.JSONEq(t, `{"delivered":1}`, rec.Body.String())

	rec = postHook(t, handler, "/hooks/shared", `{"id":"2"}`, map[string]string{WebhookSecretHeader: "s3cret"})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.JSONEq(t, `{"delivered":2}`, rec.Body.String())

	ctx := context.Background()
	_, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "alpha", SourceKind: WebhookSourceKind, SourceScope: "hook-a", ExternalID: "2",
	})
	require.NoError(t, err)
	_, err = db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "beta", SourceKind: WebhookSourceKind, SourceScope: "hook-b", ExternalID: "2",
	})
	require.NoError(t, err)
}

func TestWebhookListenerRejections(t *testing.T) {
	disabledNode := webhookFlow("off-node", "hook", "off-node-path", "")
	disabledNode.Nodes[0].Disabled = true
	disabledFlow := webhookFlow("off-flow", "hook", "off-flow-path", "")
	disabledFlow.Enabled = false

	listener, _, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", ""), disabledNode, disabledFlow})
	handler := listener.Handler()

	assert.Equal(t, http.StatusNotFound, postHook(t, handler, "/hooks/nope", `{}`, nil).Code)
	assert.Equal(t, http.StatusNotFound, postHook(t, handler, "/hooks/off-node-path", `{}`, nil).Code)
	assert.Equal(t, http.StatusNotFound, postHook(t, handler, "/hooks/off-flow-path", `{}`, nil).Code)
	assert.Equal(t, http.StatusBadRequest, postHook(t, handler, "/hooks/ci", `{not json`, nil).Code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, postHook(t, handler, "/hooks/ci", `{"pad":"`+strings.Repeat("x", maxWebhookBodyBytes)+`"}`, nil).Code)

	req := httptest.NewRequest(http.MethodGet, "/hooks/ci", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
}

func TestWebhookListenerStartStop(t *testing.T) {
	listener, _, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	require.NoError(t, listener.Start())
	defer listener.Stop()

	require.True(t, listener.Running())
	require.Positive(t, listener.Port())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/hooks/ci", listener.Port()), "application/json", strings.NewReader(`{"id":"live"}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, string(payload))
}

func TestMissingFeedItemFields(t *testing.T) {
	assert.Empty(t, MissingFeedItemFields([]byte(`{"id":"1","kind":"PR","repo":"o/r","title":"t","url":"https://x"}`)))
	assert.Equal(t, []string{"kind", "repo"}, MissingFeedItemFields([]byte(`{"id":"1","kind":7,"title":"t","url":"https://x"}`)))
	assert.Equal(t, []string{"id", "kind", "repo", "title", "url"}, MissingFeedItemFields([]byte(`[1,2,3]`)))
}

// --- Classifier-level tests: pure webhookClassifier.Classify calls, no DB. ---

func TestWebhookClassifier_FirstDeliveryActiveState(t *testing.T) {
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 100, Payload: []byte(`{"id":"x","state":"open"}`)}
	got := webhookClassifier{}.Classify(nil, current)

	assert.Equal(t, "received", got.Kind)
	assert.Equal(t, store.TransitionNone, got.Transition)
	assert.Equal(t, store.AttentionActivity, got.Attention)
	assert.Equal(t, store.LifecycleActive, got.Lifecycle)
	assert.Equal(t, "open", got.SourceState)
	assert.Equal(t, "x@100", got.OccurrenceKey)
	assert.Empty(t, got.ArchivedReason)
}

func TestWebhookClassifier_MissingStateStaysActive(t *testing.T) {
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 100, Payload: []byte(`{"id":"x"}`)}
	got := webhookClassifier{}.Classify(nil, current)
	assert.Equal(t, store.LifecycleActive, got.Lifecycle)
	assert.Empty(t, got.SourceState)

	nonObject := store.Observation{ExternalID: "y", Title: "t", ObservedAt: 100, Payload: []byte(`[1,2,3]`)}
	got = webhookClassifier{}.Classify(nil, nonObject)
	assert.Equal(t, store.LifecycleActive, got.Lifecycle)
	assert.Empty(t, got.SourceState)
}

func TestWebhookClassifier_FirstDeliveryAlreadyTerminal(t *testing.T) {
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 100, Payload: []byte(`{"id":"x","state":"done"}`)}
	got := webhookClassifier{}.Classify(nil, current)

	assert.Equal(t, "received", got.Kind)
	assert.Equal(t, store.LifecycleTerminal, got.Lifecycle)
	assert.Equal(t, store.TransitionNone, got.Transition, "first-seen terminal is not auto-archived, matching GitHub")
	assert.Equal(t, "done", got.SourceState)
	assert.Empty(t, got.ArchivedReason)
}

func TestWebhookClassifier_EntersTerminalCaseInsensitive(t *testing.T) {
	prev := store.Observation{ExternalID: "x", Payload: []byte(`{"id":"x","state":"open"}`)}
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 200, Payload: []byte(`{"id":"x","state":"Resolved"}`)}
	got := webhookClassifier{}.Classify(&prev, current)

	assert.Equal(t, "resolved", got.Kind)
	assert.Equal(t, "Resolved", got.Summary)
	assert.Equal(t, store.TransitionEnteredTerminal, got.Transition)
	assert.Equal(t, store.AttentionActivity, got.Attention)
	assert.Equal(t, "resolved", got.ArchivedReason)
	assert.Equal(t, "resolved", got.SourceState)
	assert.Equal(t, store.LifecycleTerminal, got.Lifecycle)
}

func TestWebhookClassifier_LeavesTerminalReopens(t *testing.T) {
	prev := store.Observation{ExternalID: "x", Payload: []byte(`{"id":"x","state":"closed"}`)}
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 200, Payload: []byte(`{"id":"x","state":"open"}`)}
	got := webhookClassifier{}.Classify(&prev, current)

	assert.Equal(t, "reopened", got.Kind)
	assert.Equal(t, "Reopened", got.Summary)
	assert.Equal(t, store.TransitionLeftTerminal, got.Transition)
	assert.Equal(t, store.LifecycleActive, got.Lifecycle)
	assert.Empty(t, got.ArchivedReason)
}

func TestWebhookClassifier_UnchangedActiveStateIsUpdated(t *testing.T) {
	prev := store.Observation{ExternalID: "x", Payload: []byte(`{"id":"x","n":1}`)}
	current := store.Observation{ExternalID: "x", Title: "t", ObservedAt: 200, Payload: []byte(`{"id":"x","n":2}`)}
	got := webhookClassifier{}.Classify(&prev, current)

	assert.Equal(t, "updated", got.Kind)
	assert.Equal(t, current.Title, got.Summary)
	assert.Equal(t, store.TransitionNone, got.Transition)
	assert.Equal(t, store.LifecycleActive, got.Lifecycle)
}

// --- Listener-level tests: real SQLite through IngestObservation + applyTransition. ---

func TestWebhookListenerRedeliveryEntersTerminalArchivesItem(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"open"}`, nil).Code)
	time.Sleep(2 * time.Millisecond) // distinct ObservedAt so the two deliveries get distinct occurrence keys
	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"Resolved"}`, nil).Code)

	ctx := context.Background()
	item, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "x",
	})
	require.NoError(t, err)
	assert.True(t, item.ArchivedAt.Valid)
	assert.Equal(t, "system", item.ArchivedActor.String)
	assert.Equal(t, "resolved", item.ArchivedReason.String)
	assert.Equal(t, "resolved", item.SourceState.String)
	assert.Equal(t, "terminal", item.Lifecycle)

	events, err := db.Queries().ListInboxEventsByItem(ctx, store.ListInboxEventsByItemParams{ItemID: item.ID, Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, "resolved", events[0].Kind, "latest event is first (ORDER BY id DESC)")
}

func TestWebhookListenerRedeliveryLeavesTerminalResurfaces(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"open"}`, nil).Code)
	time.Sleep(2 * time.Millisecond) // distinct ObservedAt so each delivery gets a distinct occurrence key
	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"closed"}`, nil).Code)
	time.Sleep(2 * time.Millisecond)
	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"open"}`, nil).Code)

	ctx := context.Background()
	item, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "x",
	})
	require.NoError(t, err)
	assert.False(t, item.ArchivedAt.Valid, "reopen resurfaces the item")
	assert.NotEqual(t, int64(0), item.Unread)
	assert.Equal(t, "active", item.Lifecycle)
	assert.Equal(t, "open", item.SourceState.String)

	events, err := db.Queries().ListInboxEventsByItem(ctx, store.ListInboxEventsByItemParams{ItemID: item.ID, Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, "reopened", events[0].Kind)
}

func TestWebhookListenerFirstDeliveryTerminalNotArchived(t *testing.T) {
	listener, db, _ := newWebhookTestListener(t, fakeFlows{webhookFlow("triage", "hook", "ci", "")})
	handler := listener.Handler()

	require.Equal(t, http.StatusAccepted, postHook(t, handler, "/hooks/ci", `{"id":"x","title":"t","state":"done"}`, nil).Code)

	ctx := context.Background()
	item, err := db.Queries().GetInboxItemByExternalID(ctx, store.GetInboxItemByExternalIDParams{
		ProfileID: "triage", SourceKind: WebhookSourceKind, SourceScope: "hook", ExternalID: "x",
	})
	require.NoError(t, err)
	assert.Equal(t, "terminal", item.Lifecycle)
	assert.False(t, item.ArchivedAt.Valid, "terminal on arrival is not auto-archived")
}
