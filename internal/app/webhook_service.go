package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// WebhookService owns the local webhook listener's configuration and the
// per-node capture the editor shows. The listener is nil when webhooks are
// disabled, or in mock modes without an explicit port claim; the configured
// port still describes the endpoint a live run would serve.
type WebhookService struct {
	db       *store.DB
	listener *webhook.Listener
	port     int
}

func newWebhookService(db *store.DB, listener *webhook.Listener, port int) *WebhookService {
	return &WebhookService{db: db, listener: listener, port: port}
}

// WebhookState joins the persisted configuration with this session's running
// listener, so a caller sees what is configured and what is live in one read.
type WebhookState struct {
	Enabled bool
	Port    int
	PortMin int
	PortMax int
	// PortOverridden reports that the env override is in force, in which case
	// Port is the override and editing it has no effect.
	PortOverridden bool
	// Running and BoundPort describe this session's listener. BoundPort is 0
	// when it never bound.
	Running    bool
	BoundPort  int
	StartError string
	// RestartRequired reports that the persisted configuration and the running
	// listener disagree — both toggles only take effect at startup.
	RestartRequired bool
}

// Endpoint reports the listener's live state and the port endpoints are
// served on.
func (s *WebhookService) Endpoint(context.Context) (running bool, port int) {
	if s.listener != nil && s.listener.Running() {
		return true, s.listener.Port()
	}
	return false, s.port
}

// State returns the persisted configuration alongside the listener's state.
func (s *WebhookService) State(ctx context.Context) (WebhookState, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return WebhookState{}, Wrap(err, KindInternal, "reading settings")
	}
	port, err := settings.ResolveWebhookPort(ctx, cfg)
	if err != nil {
		return WebhookState{}, Wrap(err, KindUnavailable, "resolving the webhook port")
	}

	enabled := cfg.WebhookEnabledOrDefault()
	state := WebhookState{
		Enabled:        enabled,
		Port:           port,
		PortMin:        settings.WebhookPortMin,
		PortMax:        settings.WebhookPortMax,
		PortOverridden: settings.WebhookPortOverride() > 0,
	}
	if s.listener != nil {
		state.Running = s.listener.Running()
		if err := s.listener.StartError(); err != nil {
			state.StartError = err.Error()
		}
	}
	if state.Running {
		state.BoundPort = s.listener.Port()
	}
	state.RestartRequired = enabled != state.Running || (state.Running && state.BoundPort != port)
	return state, nil
}

// SetState persists the enable toggle and port. Neither is applied to the
// running listener: both are startup-time decisions, and State reports the
// pending restart.
func (s *WebhookService) SetState(_ context.Context, enabled bool, port int) error {
	if !settings.ValidWebhookPort(port) {
		return Errorf(KindInvalid, "port must be between %d and %d", 1024, 65535)
	}
	current, err := settings.LoadSettings()
	if err != nil {
		return Wrap(err, KindInternal, "reading settings")
	}
	current.WebhookEnabled = &enabled
	current.WebhookPort = port
	return Wrap(settings.SaveSettings(current), KindInternal, "saving settings")
}

// GeneratePort returns a fresh random port from the generation range without
// persisting it: it is offered as a candidate, and saving is what commits it.
func (s *WebhookService) GeneratePort(ctx context.Context) (int, error) {
	port, err := settings.AllocateWebhookPort(ctx)
	return port, Wrap(err, KindUnavailable, "allocating a webhook port")
}

// WebhookCapture is one webhook-source node's most recent delivery.
// ReceivedAt of 0 means nothing has been captured yet.
type WebhookCapture struct {
	ReceivedAt    int64
	Body          []byte
	FeedShaped    bool
	MissingFields []string
}

// Capture returns the last request body posted to a webhook-source node, with
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
