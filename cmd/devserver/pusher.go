package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// pushTimeout bounds one webhook delivery. Targets are local desktop
// instances, so anything slower than this is hung rather than busy.
const pushTimeout = 10 * time.Second

// secretHeader is the header the desktop's webhook listener authenticates on.
// It must match internal/desktop/pipeline.WebhookSecretHeader; devserver
// declares it rather than importing so this dev tool stays decoupled from the
// app's internal packages.
const secretHeader = "X-Hive-Secret"

// pushResultLimit bounds the in-memory delivery log.
const pushResultLimit = 25

// Pusher delivers JSON payloads to configured webhook endpoints — in practice
// a desktop instance's local webhook listener, whose base URL the app shows
// under Settings ▸ Webhooks and whose path comes from a webhook-source node.
//
// This is the second, independent way to drive the desktop: the proxy makes
// GitHub say something different, while the pusher injects an event that never
// came from GitHub at all.
type Pusher struct {
	targets  map[string]WebhookTarget
	order    []string
	payloads map[string]map[string]any
	client   *http.Client
	logger   zerolog.Logger

	mu     sync.Mutex
	recent []PushResult
}

// PushResult is one delivery attempt, kept for the dashboard.
type PushResult struct {
	At       time.Time `json:"at"`
	Target   string    `json:"target"`
	URL      string    `json:"url"`
	Payload  string    `json:"payload"`
	Status   int       `json:"status"`
	Response string    `json:"response"`
	Error    string    `json:"error,omitempty"`
}

func NewPusher(cfg WebhookConfig, logger zerolog.Logger) *Pusher {
	targets := make(map[string]WebhookTarget, len(cfg.Targets))
	order := make([]string, 0, len(cfg.Targets))
	for _, target := range cfg.Targets {
		targets[target.Name] = target
		order = append(order, target.Name)
	}
	return &Pusher{
		targets:  targets,
		order:    order,
		payloads: cfg.Payloads,
		client:   &http.Client{Timeout: pushTimeout},
		logger:   logger,
	}
}

// Targets returns the configured targets in declaration order.
func (p *Pusher) Targets() []WebhookTarget {
	out := make([]WebhookTarget, 0, len(p.order))
	for _, name := range p.order {
		target := p.targets[name]
		// The secret authenticates against a dev instance, but there is no
		// reason to echo it into the dashboard or the control API.
		target.Secret = ""
		out = append(out, target)
	}
	return out
}

// PayloadNames returns the configured payload names, sorted.
func (p *Pusher) PayloadNames() []string {
	out := make([]string, 0, len(p.payloads))
	for name := range p.payloads {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Payload returns a copy of a named payload.
func (p *Pusher) Payload(name string) (map[string]any, bool) {
	payload, ok := p.payloads[name]
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		out[k] = v
	}
	return out, true
}

// Recent returns the most recent delivery attempts, newest first.
func (p *Pusher) Recent() []PushResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PushResult, len(p.recent))
	copy(out, p.recent)
	return out
}

// PushNamed delivers a configured payload, with optional per-call overrides
// merged over it. Overrides are what make a named payload reusable: push
// "pr-opened" once, then push it again with {"state":"resolved"} to drive the
// same item to a terminal state.
func (p *Pusher) PushNamed(ctx context.Context, targetName, payloadName string, overrides map[string]any) (PushResult, error) {
	payload, ok := p.Payload(payloadName)
	if !ok {
		return PushResult{}, fmt.Errorf("no payload named %q", payloadName)
	}
	for key, value := range overrides {
		payload[key] = value
	}
	return p.Push(ctx, targetName, payloadName, payload)
}

// Push delivers an arbitrary JSON object to a configured target. label names
// the payload for the delivery log only.
func (p *Pusher) Push(ctx context.Context, targetName, label string, payload map[string]any) (PushResult, error) {
	target, ok := p.targets[targetName]
	if !ok {
		return PushResult{}, fmt.Errorf("no webhook target named %q", targetName)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return PushResult{}, fmt.Errorf("encode payload: %w", err)
	}

	result := PushResult{At: time.Now().UTC(), Target: target.Name, URL: target.URL, Payload: label}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return p.finish(result, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if target.Secret != "" {
		req.Header.Set(secretHeader, target.Secret)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return p.finish(result, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	result.Status = resp.StatusCode
	result.Response = string(bytes.TrimSpace(responseBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return p.finish(result, fmt.Errorf("target returned HTTP %d: %s", resp.StatusCode, result.Response))
	}
	p.logger.Info().Str("target", target.Name).Str("payload", label).
		Int("status", resp.StatusCode).Msg("webhook pushed")
	return p.finish(result, nil)
}

// finish records a delivery attempt and returns it alongside its error.
func (p *Pusher) finish(result PushResult, err error) (PushResult, error) {
	if err != nil {
		result.Error = err.Error()
		p.logger.Warn().Err(err).Str("target", result.Target).Msg("webhook push failed")
	}
	p.mu.Lock()
	p.recent = append([]PushResult{result}, p.recent...)
	if len(p.recent) > pushResultLimit {
		p.recent = p.recent[:pushResultLimit]
	}
	p.mu.Unlock()
	return result, err
}
