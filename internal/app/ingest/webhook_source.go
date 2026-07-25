package ingest

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// WebhookSourceKind is the inbox source_kind for webhook observations.
const WebhookSourceKind = "webhook"

// WebhookPathPrefix is the URL prefix every webhook-source node's path is
// served under, leaving the listener's root free for future control routes.
const WebhookPathPrefix = "/hooks/"

// WebhookSecretHeader carries a webhook-source node's shared secret. The
// header is compared in constant time against the node's configured value.
const WebhookSecretHeader = "X-Hive-Secret"

// maxWebhookBodyBytes caps a webhook request body. Payloads are stored
// verbatim as the inbox item payload and in webhook_capture, so the cap
// bounds both.
const maxWebhookBodyBytes = 1 << 20

// webhookTerminalStates are the canonical `state` values that end a webhook
// item's lifecycle. Comparison is case-insensitive; any other or absent
// state keeps the item active (a stateless webhook behaves exactly as
// before: manual triage only).
var webhookTerminalStates = map[string]bool{"resolved": true, "closed": true, "done": true}

// decodeWebhookState extracts the canonical top-level `state` string from a
// delivery payload: lowercased and trimmed; "" for non-object payloads or a
// missing/non-string state. Delegates to the shared canonicalFields decode
// (action_item.go) — no second copy of the canonical-field parsing.
func decodeWebhookState(payload []byte) string {
	_, _, state := canonicalFields(payload)
	return strings.ToLower(strings.TrimSpace(state))
}

// webhookClassifier is the source-side classifier for webhook observations.
// It reads the canonical top-level `state` and maps it to lifecycle exactly
// like githubClassifier: a first delivery is "received" (terminal on arrival
// stays unarchived, matching GitHub); a delivery that newly enters a
// terminal state system-archives with the state as both the event kind and
// the archive reason; a delivery that leaves a terminal state resurfaces as
// "reopened"; anything else is "updated" activity. An unchanged re-delivery
// never reaches classification — IngestObservation skips it on the
// source-head comparison. The occurrence key is per delivery so downstream
// action dedup fires once per change.
type webhookClassifier struct{}

func (webhookClassifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	state := decodeWebhookState(current.Payload)
	curTerminal := webhookTerminalStates[state]
	lifecycle := store.LifecycleActive
	if curTerminal {
		lifecycle = store.LifecycleTerminal
	}
	out := store.Classification{
		Kind:          "updated",
		Transition:    store.TransitionNone,
		Attention:     store.AttentionActivity,
		Lifecycle:     lifecycle,
		SourceState:   state,
		OccurrenceKey: current.ExternalID + "@" + strconv.FormatInt(current.ObservedAt, 10),
		Summary:       current.Title,
	}
	if previous == nil {
		out.Kind = "received"
		return out
	}
	prevTerminal := webhookTerminalStates[decodeWebhookState(previous.Payload)]
	switch {
	case !prevTerminal && curTerminal:
		out.Kind, out.Summary, out.Transition, out.ArchivedReason = state, titleCase(state), store.TransitionEnteredTerminal, state
	case prevTerminal && !curTerminal:
		out.Kind, out.Summary, out.Transition = "reopened", "Reopened", store.TransitionLeftTerminal
	}
	return out
}

// WebhookListener is the desktop's local webhook ingress: a localhost-only
// HTTP server whose /hooks/<path> routes are resolved per request from the
// current flow set, so adding, editing, or removing webhook-source nodes
// takes effect without a restart (the same late-binding posture as the
// producer's per-tick SourceLister). It bypasses the poll Producer entirely:
// a delivery calls IngestObservation directly — the production source
// boundary — then appends the topic's authoritative snapshot so membership
// replay keeps webhook-fed feeds intact across deploys and restarts.
type WebhookListener struct {
	db         *store.DB
	flows      FlowLister
	onAppended func(nextOffset int64)
	logger     zerolog.Logger
	recorder   activity.Recorder

	port     int
	server   *http.Server
	listener net.Listener
	startErr error
}

// NewWebhookListener builds a listener bound to 127.0.0.1:port at Start.
// onAppended fires after a delivery appends event-log rows, with the offset
// of the last row (main.go wires the Wails "log:appended" wake-up).
func NewWebhookListener(db *store.DB, flows FlowLister, port int, onAppended func(nextOffset int64), logger zerolog.Logger) *WebhookListener {
	return &WebhookListener{db: db, flows: flows, port: port, onAppended: onAppended, logger: logger}
}

// SetRecorder attaches an activity recorder so ingest failures surface in the
// Activity view. Set once at wiring time, before Start.
func (l *WebhookListener) SetRecorder(r activity.Recorder) { l.recorder = r }

// Start binds 127.0.0.1 and serves in a goroutine. A bind failure (port in
// use) is returned to the caller, which logs and continues — a busy webhook
// port must never take the desktop app down with it — and is retained for
// StartError so settings can surface it instead of leaving it in the log.
func (l *WebhookListener) Start() error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(l.port)))
	if err != nil {
		l.startErr = fmt.Errorf("webhook listener: %w", err)
		return l.startErr
	}
	l.listener = ln
	l.server = &http.Server{Handler: l.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := l.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.logger.Error().Err(err).Msg("webhook listener stopped unexpectedly")
		}
	}()
	l.logger.Info().Int("port", l.Port()).Msg("webhook listener started")
	return nil
}

// Stop gracefully shuts the server down, letting in-flight ingests finish.
func (l *WebhookListener) Stop() {
	if l.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := l.server.Shutdown(ctx); err != nil {
		l.logger.Warn().Err(err).Msg("webhook listener shutdown")
	}
}

// Running reports whether Start succeeded and the listener is bound.
func (l *WebhookListener) Running() bool { return l.listener != nil }

// StartError returns why Start failed to bind, or nil if it never failed.
func (l *WebhookListener) StartError() error { return l.startErr }

// Port returns the bound TCP port once Running, else the configured port.
// They differ only when the listener was constructed with port 0 (tests).
func (l *WebhookListener) Port() int {
	if l.listener != nil {
		if addr, ok := l.listener.Addr().(*net.TCPAddr); ok {
			return addr.Port
		}
	}
	return l.port
}

// Handler returns the listener's route handler. Exposed (rather than only
// being installed by Start) so tests can drive deliveries through
// httptest without binding a real port.
func (l *WebhookListener) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(WebhookPathPrefix, l.handleHook)
	return mux
}

// webhookTarget is one enabled webhook-source node matched by path.
type webhookTarget struct {
	flowID string
	nodeID string
	secret string
	policy store.ResurfacePolicy
}

func (t webhookTarget) topic() string { return "source:" + t.flowID + "/" + t.nodeID }

// resolveTargets returns every enabled webhook-source node declaring path,
// across all enabled flows. Several nodes may share a path — each receives
// the delivery, so one sender can fan into multiple flows.
func (l *WebhookListener) resolveTargets(path string) []webhookTarget {
	var out []webhookTarget
	for _, f := range l.flows.List() {
		if !f.Enabled {
			continue
		}
		for _, node := range f.Nodes {
			if node.Disabled || node.Type != "webhook-source" {
				continue
			}
			cfg, ok := node.Config.(*flow.WebhookSourceConfig)
			if !ok || cfg.Path != path {
				continue
			}
			out = append(out, webhookTarget{
				flowID: f.ID,
				nodeID: node.ID,
				secret: cfg.Secret,
				policy: store.ResurfacePolicy(f.Resurface),
			})
		}
	}
	return out
}

func (l *WebhookListener) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, WebhookPathPrefix)
	targets := l.resolveTargets(path)
	if len(targets) == 0 {
		http.Error(w, "no webhook endpoint at this path", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "reading request body", http.StatusBadRequest)
		return
	}
	if !json.Valid(body) {
		http.Error(w, "request body must be valid JSON", http.StatusBadRequest)
		return
	}

	secret := r.Header.Get(WebhookSecretHeader)
	authorized := targets[:0:0]
	for _, t := range targets {
		if t.secret == "" || subtle.ConstantTimeCompare([]byte(t.secret), []byte(secret)) == 1 {
			authorized = append(authorized, t)
		}
	}
	if len(authorized) == 0 {
		http.Error(w, "invalid or missing "+WebhookSecretHeader+" header", http.StatusUnauthorized)
		return
	}

	now := time.Now().UnixMilli()
	key, title, url := webhookIdentity(path, body)

	delivered := 0
	var lastOffset int64
	for _, t := range authorized {
		offset, err := l.ingest(r.Context(), t, key, title, url, body, now)
		if err != nil {
			l.logger.Error().Err(err).Str("topic", t.topic()).Msg("webhook ingest failed")
			l.record(r.Context(), activity.RefreshFailed(t.flowID+"/"+t.nodeID, err.Error()))
			continue
		}
		delivered++
		if offset > lastOffset {
			lastOffset = offset
		}
	}
	if delivered == 0 {
		http.Error(w, "ingest failed", http.StatusInternalServerError)
		return
	}
	if lastOffset > 0 && l.onAppended != nil {
		l.onAppended(lastOffset)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]int{"delivered": delivered})
}

// ingest persists one delivery for one target: the observation (which
// appends the routed event-log row when the payload changed), the capture
// row (always — it is "the last request", independent of deduplication),
// and, after a write, the topic's complete authoritative snapshot so
// startup/deploy membership replay resolves this source's feed claims. It
// returns the offset of the last event-log row it appended (0 when the
// payload was unchanged).
func (l *WebhookListener) ingest(ctx context.Context, t webhookTarget, key, title, url string, body []byte, now int64) (int64, error) {
	topic := t.topic()

	result, err := l.db.IngestObservation(ctx, webhookClassifier{}, store.IngestObservationParams{
		ProfileID: t.flowID,
		Topic:     topic,
		Policy:    t.policy,
		Current: store.Observation{
			ExternalID:  key,
			Title:       title,
			URL:         url,
			SourceKind:  WebhookSourceKind,
			SourceScope: t.nodeID,
			ObservedAt:  now,
			Payload:     body,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("ingesting webhook observation %q: %w", key, err)
	}

	if err := l.db.Queries().UpsertWebhookCapture(ctx, store.UpsertWebhookCaptureParams{
		Topic: topic, ReceivedAt: now, Body: body,
	}); err != nil {
		// The capture only powers editor affordances; losing it must not
		// fail a delivery that already ingested.
		l.logger.Warn().Err(err).Str("topic", topic).Msg("webhook capture write failed")
	}

	if !result.Wrote {
		return 0, nil
	}

	rows, err := l.db.Queries().ListUnarchivedInboxItemsBySource(ctx, store.ListUnarchivedInboxItemsBySourceParams{
		ProfileID: t.flowID, SourceKind: WebhookSourceKind, SourceScope: t.nodeID,
	})
	if err != nil {
		return result.Offset, fmt.Errorf("listing webhook snapshot items for %q: %w", topic, err)
	}
	items := make([]store.SnapshotItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, store.SnapshotItem{Key: row.ExternalID, Payload: row.Payload})
	}
	offset, err := l.db.AppendSnapshot(ctx, topic, WebhookSourceKind, t.nodeID, items)
	if err != nil {
		return result.Offset, fmt.Errorf("appending webhook snapshot for %q: %w", topic, err)
	}
	return offset, nil
}

func (l *WebhookListener) record(ctx context.Context, event activity.Event) {
	if l.recorder == nil {
		return
	}
	l.recorder.Record(ctx, event)
}

// webhookIdentity derives the observation's stable key and its promoted
// title/url columns from the delivered JSON. A top-level "id" (string or
// number) is the stable identity — re-deliveries with the same id update the
// same inbox item. Without one, the key is the body's content hash, so an
// exact duplicate delivery deduplicates and any changed body is a new item.
// "title" and "url" are promoted when present so the item renders in feeds;
// everything else stays inside the opaque payload.
func webhookIdentity(path string, body []byte) (key, title, url string) {
	var fields map[string]json.RawMessage
	// Non-object JSON (arrays, scalars) is accepted; it just has no fields
	// to promote.
	_ = json.Unmarshal(body, &fields)

	if raw, ok := fields["id"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && strings.TrimSpace(s) != "" {
			key = strings.TrimSpace(s)
		} else {
			var n json.Number
			if err := json.Unmarshal(raw, &n); err == nil {
				key = n.String()
			}
		}
	}
	if key == "" {
		sum := sha256.Sum256(body)
		key = hex.EncodeToString(sum[:])
	}

	if raw, ok := fields["title"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			title = strings.TrimSpace(s)
		}
	}
	if title == "" {
		short := key
		if len(short) > 12 {
			short = short[:12]
		}
		title = path + " · " + short
	}

	if raw, ok := fields["url"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
				url = s
			}
		}
	}
	return key, title, url
}

// feedItemFields lists the render-critical subset of the canonical item
// contract (docs/decisions/0008-canonical-item-contract.md): the fields the
// feed UI needs for a first-party-quality row. The remaining contract
// fields (num, author, body, labels, state, updatedAt) are optional
// enrichment — state notably drives lifecycle — and their absence is not a
// shape warning. Advisory only — never enforced at ingress.
var feedItemFields = []string{"id", "kind", "repo", "title", "url"}

// MissingFeedItemFields returns which of the feed-item fields the payload
// lacks (absent, or present but not a non-empty string). A nil/empty return
// means the payload will render like a first-party feed item.
func MissingFeedItemFields(payload []byte) []string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return append([]string(nil), feedItemFields...)
	}
	var missing []string
	for _, name := range feedItemFields {
		raw, ok := fields[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}
