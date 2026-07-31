package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// maxBodyBytes caps a webhook request body. Payloads are stored verbatim as
// the inbox item payload and in webhook_capture, so the cap bounds both.
const maxBodyBytes = 1 << 20

// Instances resolves every live webhook source instance. It is called per
// request rather than fixed at construction, so adding, editing, or removing
// a webhook source node takes effect without a restart — the same
// late-binding posture as the producer's per-tick source resolution.
//
// It is a function of connector instances rather than of flows so this
// package never imports the flow package: the flow set is walked by whoever
// owns the registry, and the connector only sees its own instances.
type Instances func() []connector.Instance

// Listener is the desktop's local webhook ingress: a localhost-only HTTP
// server whose /hooks/<path> routes are resolved per request. It bypasses the
// poll producer entirely — a delivery calls IngestObservation directly, the
// production source boundary, then appends the topic's authoritative snapshot
// so membership replay keeps webhook-fed feeds intact across deploys and
// restarts.
type Listener struct {
	db         *store.DB
	instances  Instances
	onAppended func(nextOffset int64)
	logger     zerolog.Logger
	recorder   activity.Recorder

	mu       sync.Mutex
	host     string
	port     int
	server   *http.Server
	listener net.Listener
	startErr error
	mounts   map[string]http.Handler
}

// NewListener builds a listener bound to host:port at Start. Configuration
// validation limits host to loopback. onAppended fires after a delivery
// appends event-log rows so the core can wake the flow engine.
func NewListener(db *store.DB, instances Instances, host string, port int, onAppended func(nextOffset int64), logger zerolog.Logger) *Listener {
	return &Listener{db: db, instances: instances, host: host, port: port, onAppended: onAppended, logger: logger, mounts: make(map[string]http.Handler)}
}

// SetRecorder attaches an activity recorder so ingest failures surface in the
// Activity view. Set once at wiring time, before Start.
func (l *Listener) SetRecorder(r activity.Recorder) { l.recorder = r }

// Start binds and serves. Errors when already running. It resets StartError and
// rebuilds the mux from the current mounts on every call, so Start after Stop
// rebinds and re-serves.
func (l *Listener) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listener != nil {
		return fmt.Errorf("webhook listener already running")
	}

	l.startErr = nil
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(l.host, strconv.Itoa(l.port)))
	if err != nil {
		l.startErr = fmt.Errorf("webhook listener: %w", err)
		return l.startErr
	}
	server := &http.Server{Handler: l.handlerLocked(), ReadHeaderTimeout: 5 * time.Second}
	l.listener = ln
	l.server = server
	host, port := l.host, listenerPort(ln)
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.logger.Error().Err(err).Msg("webhook listener stopped unexpectedly")
		}
	}()
	l.logger.Info().Str("host", host).Int("port", port).Msg("webhook listener started")
	return nil
}

// Stop gracefully shuts down, letting in-flight ingests finish until ctx is
// done; on a drain timeout it force-Closes so the port actually frees for a
// rebind. Idempotent by state: not running is a nil no-op. After Stop,
// Running reports false and a later Start rebinds.
func (l *Listener) Stop(ctx context.Context) error {
	l.mu.Lock()
	server := l.server
	if server == nil {
		l.mu.Unlock()
		return nil
	}
	l.server = nil
	l.listener = nil
	l.mu.Unlock()

	err := server.Shutdown(ctx)
	if err != nil {
		_ = server.Close()
	}
	return err
}

// SetAddr changes the address the next Start binds. It does not touch a
// running server; restart policy lives in the caller.
func (l *Listener) SetAddr(host string, port int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.host, l.port = host, port
}

// Running reports whether Start succeeded and the listener is bound.
func (l *Listener) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.listener != nil
}

// StartError returns why Start failed to bind, or nil if it never failed.
func (l *Listener) StartError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.startErr
}

// Port returns the bound TCP port once Running, else the configured port.
func (l *Listener) Port() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listener != nil {
		return listenerPort(l.listener)
	}
	return l.port
}

func listenerPort(listener net.Listener) int {
	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// Host returns the actual bound address once running, else the configured host.
func (l *Listener) Host() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listener != nil {
		if addr, ok := l.listener.Addr().(*net.TCPAddr); ok {
			return addr.IP.String()
		}
	}
	return l.host
}

// MountAPI mounts (or replaces) a handler at prefix. Callable any time; a
// mount added while running takes effect at the next restart.
func (l *Listener) MountAPI(prefix string, h http.Handler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mounts[prefix] = h
}

// UnmountAPI removes a prefix so route absence holds after the next restart —
// how terminal-off takes the stream mount away (ADR 0037).
func (l *Listener) UnmountAPI(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.mounts, prefix)
}

// Handler returns the listener's route handler. Exposed (rather than only
// being installed by Start) so tests can drive deliveries through httptest
// without binding a real port.
func (l *Listener) Handler() http.Handler {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.handlerLocked()
}

func (l *Listener) handlerLocked() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathPrefix, l.handleHook)
	for prefix, handler := range l.mounts {
		mux.Handle(prefix, handler)
	}
	return mux
}

// target is one enabled webhook source node matched by path.
type target struct {
	instance connector.Instance
	secret   string
}

// resolveTargets returns every enabled webhook source instance declaring
// path. Several may share a path — each receives the delivery, so one sender
// can fan into multiple flows.
func (l *Listener) resolveTargets(path string) []target {
	var out []target
	for _, inst := range l.instances() {
		cfg, ok := inst.Config.(*Config)
		if !ok || cfg.Path != path {
			continue
		}
		out = append(out, target{instance: inst, secret: cfg.Secret})
	}
	return out
}

func (l *Listener) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, PathPrefix)
	targets := l.resolveTargets(path)
	if len(targets) == 0 {
		http.Error(w, "no webhook endpoint at this path", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
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

	secret := r.Header.Get(SecretHeader)
	authorized := targets[:0:0]
	for _, t := range targets {
		if t.secret == "" || subtle.ConstantTimeCompare([]byte(t.secret), []byte(secret)) == 1 {
			authorized = append(authorized, t)
		}
	}
	if len(authorized) == 0 {
		http.Error(w, "invalid or missing "+SecretHeader+" header", http.StatusUnauthorized)
		return
	}

	now := time.Now().UnixMilli()
	key, title, url := identity(path, body)

	delivered := 0
	var lastOffset int64
	for _, t := range authorized {
		offset, err := l.ingest(r.Context(), t.instance, key, title, url, body, now)
		if err != nil {
			l.logger.Error().Err(err).Str("topic", t.instance.Node.Topic()).Msg("webhook ingest failed")
			l.record(r.Context(), activity.RefreshFailed(t.instance.Node.ID(), err.Error()))
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

// ingest persists one delivery for one instance: the observation (which
// appends the routed event-log row when the payload changed), the capture row
// (always — it is "the last request", independent of deduplication), and,
// after a write, the topic's complete authoritative snapshot so
// startup/deploy membership replay resolves this source's feed claims. It
// returns the offset of the last event-log row it appended (0 when the
// payload was unchanged).
func (l *Listener) ingest(ctx context.Context, inst connector.Instance, key, title, url string, body []byte, now int64) (int64, error) {
	topic := inst.Node.Topic()
	meta := inst.Metadata

	result, err := l.db.IngestObservation(ctx, inst.Classifier, store.IngestObservationParams{
		ProfileID: meta.ProfileID,
		Topic:     topic,
		Policy:    meta.Policy,
		Current: store.Observation{
			ExternalID:  key,
			Title:       title,
			URL:         url,
			SourceKind:  meta.SourceKind,
			SourceScope: meta.SourceScope,
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
		ProfileID: meta.ProfileID, SourceKind: meta.SourceKind, SourceScope: meta.SourceScope,
	})
	if err != nil {
		return result.Offset, fmt.Errorf("listing webhook snapshot items for %q: %w", topic, err)
	}
	items := make([]store.SnapshotItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, store.SnapshotItem{Key: row.ExternalID, Payload: row.Payload})
	}
	offset, err := l.db.AppendSnapshot(ctx, topic, meta.SourceKind, meta.SourceScope, items)
	if err != nil {
		return result.Offset, fmt.Errorf("appending webhook snapshot for %q: %w", topic, err)
	}
	return offset, nil
}

func (l *Listener) record(ctx context.Context, event activity.Event) {
	if l.recorder == nil {
		return
	}
	l.recorder.Record(ctx, event)
}
