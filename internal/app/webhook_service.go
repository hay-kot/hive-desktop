package app

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sourcemark"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// WebhookService owns the local webhook listener's configuration, the per-node
// capture the editor shows, and the uploaded feed-mark images a webhook node
// may show instead of a glyph. The listener is nil when webhooks are disabled,
// or in mock modes without an explicit port claim; the configured port still
// describes the endpoint a live run would serve.
type WebhookService struct {
	settings *settings.Store
	db       *store.DB
	listener *webhook.Listener
	marks    *sourcemark.Store
	// applyHTTP is App.applyHTTPSettings — SetState's scheduled apply half.
	// Nil in tests that only exercise persistence.
	applyHTTP func(settings.Settings)

	mu       sync.Mutex
	startErr error
}

func newWebhookService(settingsStore *settings.Store, db *store.DB, listener *webhook.Listener, marks *sourcemark.Store, applyHTTP func(settings.Settings)) *WebhookService {
	return &WebhookService{settings: settingsStore, db: db, listener: listener, marks: marks, applyHTTP: applyHTTP}
}

func (s *WebhookService) setStartError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startErr = err
}

func (s *WebhookService) clearStartError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startErr = nil
}

// WebhookState joins the persisted configuration with this session's running
// listener, so a caller sees what is configured and what is live in one read.
type WebhookState struct {
	Enabled bool
	Host    string
	Port    int
	PortMin int
	PortMax int
	// PortOverridden reports that the env override is in force, in which case
	// Port is the override and editing it has no effect.
	PortOverridden bool
	// Running and BoundPort describe this session's listener. BoundPort is 0
	// when it never bound.
	Running    bool
	BoundHost  string
	BoundPort  int
	StartError string
}

// Endpoint reports the listener's live state and the port endpoints are
// served on.
func (s *WebhookService) Endpoint(context.Context) (running bool, port int) {
	if s.listener != nil && s.listener.Running() {
		return true, s.listener.Port()
	}
	return false, s.settings.Current().HTTP.Port
}

// Host reports the actual bound host when running, else the configured host.
func (s *WebhookService) Host() string {
	if s.listener != nil && s.listener.Running() {
		return s.listener.Host()
	}
	return s.settings.Current().HTTP.Host
}

// State returns the persisted configuration alongside the listener's state.
func (s *WebhookService) State(context.Context) WebhookState {
	cfg := s.settings.Current()
	state := WebhookState{
		Enabled:        cfg.HTTP.Enabled,
		Host:           cfg.HTTP.Host,
		Port:           cfg.HTTP.Port,
		PortMin:        settings.WebhookPortMin,
		PortMax:        settings.WebhookPortMax,
		PortOverridden: cfg.EnvironmentOverridden(settings.EnvHTTPPort),
	}
	if s.listener != nil {
		state.Running = s.listener.Running()
		if state.Running {
			state.BoundHost = s.listener.Host()
			state.BoundPort = s.listener.Port()
		}
		if err := s.listener.StartError(); err != nil {
			state.StartError = err.Error()
		}
	}
	if state.StartError == "" {
		s.mu.Lock()
		if s.startErr != nil {
			state.StartError = s.startErr.Error()
		}
		s.mu.Unlock()
	}
	return state
}

// SetState persists the http section, then schedules the apply — the
// persist/apply split (ADR 0041) with ADR 0042's scheduled reconcile, so the
// Wails call returns before the restart runs.
func (s *WebhookService) SetState(_ context.Context, enabled bool, host string, port int) error {
	_, err := s.settings.Update(func(current *settings.Settings) error {
		current.HTTP.Enabled = enabled
		current.HTTP.Host = host
		current.HTTP.Port = port
		if err := current.Validate(); err != nil {
			return Wrap(err, KindInvalid, "validating webhook settings")
		}
		return nil
	})
	if err != nil {
		if KindOf(err) == KindInvalid {
			return err
		}
		return Wrap(err, KindInternal, "saving settings")
	}
	if s.applyHTTP != nil {
		s.applyHTTP(s.settings.Current())
	}
	return nil
}

// GeneratePort returns a fresh random port from the generation range without
// persisting it: it is offered as a candidate, and saving is what commits it.
func (s *WebhookService) GeneratePort(ctx context.Context) (int, error) {
	port, err := settings.AllocateWebhookPort(ctx)
	return port, Wrap(err, KindUnavailable, "allocating a webhook port")
}

// WebhookCapture is one sources.webhook node's most recent delivery.
// ReceivedAt of 0 means nothing has been captured yet.
type WebhookCapture struct {
	ReceivedAt    int64
	Body          []byte
	FeedShaped    bool
	MissingFields []string
}

// Capture returns the last request body posted to a sources.webhook node, with
// the non-blocking feed-shape verdict: a payload missing feed-item fields
// ingests fine but renders minimally in feeds.
func (s *WebhookService) Capture(ctx context.Context, flowID, nodeID string) (WebhookCapture, error) {
	topic := "source:" + flowID + "/" + nodeID
	row, err := s.db.Queries().GetWebhookCapture(ctx, topic)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookCapture{}, nil
	}
	if err != nil {
		return WebhookCapture{}, Wrap(err, KindInternal, "reading the webhook capture for %s", topic)
	}
	missing := webhook.MissingFeedItemFields(row.Body)
	return WebhookCapture{
		ReceivedAt:    row.ReceivedAt,
		Body:          row.Body,
		FeedShaped:    len(missing) == 0,
		MissingFields: missing,
	}, nil
}

// StoreMarkImage normalizes raw into a feed-mark PNG, stores it, and returns its
// content hash — the value a webhook node records in its `image` config. It does
// not touch the flow; the graph save records the hash.
func (s *WebhookService) StoreMarkImage(_ context.Context, raw []byte) (string, error) {
	hash, err := s.marks.Set(raw)
	if err != nil {
		return "", mapMarkImageError(err)
	}
	return hash, nil
}

// MarkImage returns the stored PNG for a mark hash, or ok=false when none is
// stored. A missing or malformed reference is not an error.
func (s *WebhookService) MarkImage(_ context.Context, hash string) (data []byte, ok bool, err error) {
	data, ok, err = s.marks.Get(hash)
	if err != nil {
		return nil, false, Wrap(err, KindInternal, "reading mark image %q", hash)
	}
	return data, ok, nil
}

// mapMarkImageError turns a normalization failure into a user-facing message.
func mapMarkImageError(err error) error {
	switch {
	case errors.Is(err, sourcemark.ErrEmpty):
		return Errorf(KindInvalid, "No image was provided.")
	case errors.Is(err, sourcemark.ErrUnsupported):
		return Errorf(KindInvalid, "That file isn't a supported image. Use PNG, JPEG, GIF, or WebP.")
	case errors.Is(err, sourcemark.ErrTooLarge):
		return Errorf(KindInvalid, "That image is too large. Choose a smaller file.")
	default:
		return Wrap(err, KindInternal, "processing image")
	}
}
