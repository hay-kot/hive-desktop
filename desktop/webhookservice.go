package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/pipelinedb"
)

// WebhookService is the Wails service exposing the local webhook listener's
// endpoint info and each webhook-source node's last captured delivery to the
// frontend node editor.
type WebhookService struct {
	db       *pipelinedb.DB
	listener *pipeline.WebhookListener
	port     int
}

// NewWebhookService wires the service. listener is nil in mock modes without
// an explicit port claim; port is the configured port either way, so the
// editor can render the endpoint URL a live run would serve.
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
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d%s", port, pipeline.WebhookPathPrefix),
	}
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
