package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/pipelinedb"
)

// WebhookService is the Wails service exposing the local webhook listener's
// endpoint info, its user-tunable settings, and each webhook-source node's
// last captured delivery to the frontend.
type WebhookService struct {
	db       *pipelinedb.DB
	listener *pipeline.WebhookListener
	port     int
}

// NewWebhookService wires the service. listener is nil when the listener is
// disabled, or in mock modes without an explicit port claim; port is the
// configured port either way, so the editor can render the endpoint URL a
// live run would serve.
func NewWebhookService(db *pipelinedb.DB, listener *pipeline.WebhookListener, port int) *WebhookService {
	return &WebhookService{db: db, listener: listener, port: port}
}

// WebhookInfo describes the local webhook listener for the node editor.
type WebhookInfo struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	BaseURL string `json:"baseUrl"`
}

// Info returns the listener's state and the base URL webhook-source paths
// are served under (endpoint URL = BaseURL + node path).
func (s *WebhookService) Info() WebhookInfo {
	port := s.port
	running := false
	if s.listener != nil && s.listener.Running() {
		port = s.listener.Port()
		running = true
	}
	return WebhookInfo{
		Running: running,
		Port:    port,
		BaseURL: webhookBaseURL(port),
	}
}

// WebhookSettings is the local listener's editable configuration joined with
// the running listener's actual state, so the settings pane can show what is
// configured and what is live in one read.
type WebhookSettings struct {
	// Enabled and Port are the persisted configuration.
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
	// PortMin and PortMax bound generated ports; the frontend reuses them to
	// label the field rather than restating the range.
	PortMin int `json:"portMin"`
	PortMax int `json:"portMax"`
	// PortOverridden reports that HIVE_DESKTOP_WEBHOOK_PORT is in force, in
	// which case Port is the override and editing it has no effect.
	PortOverridden bool `json:"portOverridden"`
	// Running, BoundPort, BaseURL, and StartError describe this session's
	// listener. BoundPort is 0 when it never bound.
	Running    bool   `json:"running"`
	BoundPort  int    `json:"boundPort"`
	BaseURL    string `json:"baseUrl"`
	StartError string `json:"startError"`
	// RestartRequired reports that the persisted configuration and the running
	// listener disagree — both toggles only take effect at startup.
	RestartRequired bool `json:"restartRequired"`
}

// Settings returns the persisted webhook configuration alongside the state of
// this session's listener.
func (s *WebhookService) Settings() (WebhookSettings, error) {
	cfg, err := settings.LoadSettings()
	if err != nil {
		return WebhookSettings{}, err
	}

	port, err := settings.ResolveWebhookPort(cfg)
	if err != nil {
		return WebhookSettings{}, err
	}
	enabled := cfg.WebhookEnabledOrDefault()

	view := WebhookSettings{
		Enabled:        enabled,
		Port:           port,
		PortMin:        settings.WebhookPortMin,
		PortMax:        settings.WebhookPortMax,
		PortOverridden: settings.WebhookPortOverride() > 0,
		BaseURL:        webhookBaseURL(port),
	}
	if s.listener != nil {
		view.Running = s.listener.Running()
		if err := s.listener.StartError(); err != nil {
			view.StartError = err.Error()
		}
	}
	if view.Running {
		view.BoundPort = s.listener.Port()
		view.BaseURL = webhookBaseURL(view.BoundPort)
	}
	view.RestartRequired = enabled != view.Running || (view.Running && view.BoundPort != port)
	return view, nil
}

// SetSettings persists the enable toggle and port, preserving all unrelated
// desktop settings. Neither is applied to the running listener: both are
// startup-time decisions, and Settings reports the pending restart.
func (s *WebhookService) SetSettings(next WebhookSettings) error {
	if !settings.ValidWebhookPort(next.Port) {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	current, err := settings.LoadSettings()
	if err != nil {
		return err
	}
	current.WebhookEnabled = &next.Enabled
	current.WebhookPort = next.Port
	return settings.SaveSettings(current)
}

// GeneratePort returns a fresh random port from the generation range without
// persisting it: the settings pane offers it as a candidate, and saving is
// what commits it.
func (s *WebhookService) GeneratePort() (int, error) {
	return settings.AllocateWebhookPort()
}

func webhookBaseURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, pipeline.WebhookPathPrefix)
}

// WebhookCaptureView is one webhook-source node's most recent delivery.
// ReceivedAt of 0 means no delivery has been captured yet.
type WebhookCaptureView struct {
	ReceivedAt    int64    `json:"receivedAt"`
	Body          string   `json:"body"`
	FeedShaped    bool     `json:"feedShaped"`
	MissingFields []string `json:"missingFields"`
}

// Capture returns the last request body POSTed to a webhook-source node,
// with the non-blocking feed-shape verdict the editor surfaces (a payload
// missing feed-item fields ingests fine but renders minimally in feeds).
func (s *WebhookService) Capture(flowID, nodeID string) (WebhookCaptureView, error) {
	topic := "source:" + flowID + "/" + nodeID
	row, err := s.db.Queries().GetWebhookCapture(context.Background(), topic)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookCaptureView{}, nil
	}
	if err != nil {
		return WebhookCaptureView{}, fmt.Errorf("reading webhook capture for %s: %w", topic, err)
	}
	missing := pipeline.MissingFeedItemFields(row.Body)
	return WebhookCaptureView{
		ReceivedAt:    row.ReceivedAt,
		Body:          string(row.Body),
		FeedShaped:    len(missing) == 0,
		MissingFields: missing,
	}, nil
}
