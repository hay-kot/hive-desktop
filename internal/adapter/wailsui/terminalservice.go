package wailsui

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// TerminalTransport is what the terminal needs that the core does not hold: the
// per-run bearer token and the path its WebSocket is mounted at. Both are
// composed in main.go and handed here (ADR 0036).
type TerminalTransport struct {
	Token      string
	StreamPath string
}

// TerminalAvailability is the frontend's only gate on terminal mode. Reason is
// user-facing prose: no tmux, tmux too old, a server build, or no loopback
// server to carry the transport.
type TerminalAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// TerminalEndpoint bootstraps the webview: control actions go to HTTPBaseURL
// with the bearer token, the data plane opens WSURL.
type TerminalEndpoint struct {
	HTTPBaseURL string `json:"httpBaseURL"`
	WSURL       string `json:"wsURL"`
	Token       string `json:"token"`
}

// TerminalService gates terminal mode and hands the webview its transport. The
// data path itself never crosses the Wails bridge.
type TerminalService struct {
	terminals *app.TerminalsService
	webhooks  *app.WebhookService

	mu        sync.RWMutex
	transport TerminalTransport
	enabled   bool
}

func NewTerminalService(terminals *app.TerminalsService, webhooks *app.WebhookService, transport TerminalTransport, enabled bool) *TerminalService {
	return &TerminalService{terminals: terminals, webhooks: webhooks, transport: transport, enabled: enabled}
}

// SetState swaps the gate and transport a terminal flip produced; Enabled,
// Available and Endpoint read under the lock. The service itself stays
// registered at application.New — only its state moves (ADR 0037 update).
//
//wails:ignore
func (s *TerminalService) SetState(enabled bool, transport TerminalTransport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled, s.transport = enabled, transport
}

// Enabled reports the experimental.terminal opt-in (ADR 0037). The frontend
// renders the way into terminal mode only when it is on; availability stays a
// separate axis, because an enabled-but-unavailable terminal explains itself
// inside the mode instead of hiding the way in.
func (s *TerminalService) Enabled(context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// Available answers even when the loopback server is down, which is why it
// composes tmux availability with the transport's own reachability.
func (s *TerminalService) Available(ctx context.Context) TerminalAvailability {
	s.mu.RLock()
	enabled := s.enabled
	s.mu.RUnlock()
	if !enabled {
		return TerminalAvailability{Reason: "Terminal mode is off. Turn it on in Settings ▸ System."}
	}
	if err := s.terminals.Available(ctx); err != nil {
		return TerminalAvailability{Reason: reasonFor(err)}
	}
	if _, err := s.Endpoint(ctx); err != nil {
		return TerminalAvailability{Reason: reasonFor(err)}
	}
	return TerminalAvailability{Available: true}
}

// Endpoint reports where the terminal server is reachable, or KindUnavailable
// while the loopback server is unbound or HTTP is disabled.
func (s *TerminalService) Endpoint(ctx context.Context) (TerminalEndpoint, error) {
	s.mu.RLock()
	transport := s.transport
	s.mu.RUnlock()
	if transport.Token == "" || transport.StreamPath == "" {
		return TerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The terminal transport is not mounted in this build.")
	}
	running, port := s.webhooks.Endpoint(ctx)
	if !running {
		return TerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The local HTTP server is not running, so the terminal has nothing to connect to. Check http.enabled in settings.yaml.")
	}
	authority := net.JoinHostPort(s.webhooks.Host(), strconv.Itoa(port))
	return TerminalEndpoint{
		HTTPBaseURL: "http://" + authority,
		WSURL:       "ws://" + authority + transport.StreamPath,
		Token:       transport.Token,
	}, nil
}

// reasonFor takes the user-facing message off a core error; anything else is
// reported verbatim rather than swallowed.
func reasonFor(err error) string {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		return appErr.Msg
	}
	return err.Error()
}
