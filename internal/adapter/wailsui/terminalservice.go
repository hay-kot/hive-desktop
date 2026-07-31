package wailsui

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// TerminalTransport is what the terminal needs that the core does not hold: the
// per-run bearer token and the paths its WebSockets are mounted at. All are
// composed in main.go and handed here (ADR 0036). PtyStreamPath is the
// process-managed backend's stream, empty when it is not mounted (ADR 0045).
type TerminalTransport struct {
	Token         string
	StreamPath    string
	PtyStreamPath string
}

// TerminalAvailability is the frontend's only gate on terminal mode. Reason is
// user-facing prose: no tmux, tmux too old, a server build, or no loopback
// server to carry the transport. PtyAvailable is a second axis rather than part
// of Available: the process-managed backend needs no tmux, so a machine without
// one can still open a terminal on it.
type TerminalAvailability struct {
	Available    bool   `json:"available"`
	Reason       string `json:"reason"`
	PtyAvailable bool   `json:"ptyAvailable"`
}

// TerminalEndpoint bootstraps the webview: control actions go to HTTPBaseURL
// with the bearer token, the data plane opens WSURL — or PtyWSURL for the
// process-managed backend, which is empty when that one is not mounted.
type TerminalEndpoint struct {
	HTTPBaseURL string `json:"httpBaseURL"`
	WSURL       string `json:"wsURL"`
	PtyWSURL    string `json:"ptyWSURL"`
	Token       string `json:"token"`
}

// TerminalService gates terminal mode and hands the webview its transport. The
// data path itself never crosses the Wails bridge.
type TerminalService struct {
	terminals *app.TerminalsService
	pty       *app.PtyTerminalsService
	webhooks  *app.WebhookService
	transport TerminalTransport
	enabled   bool
}

func NewTerminalService(terminals *app.TerminalsService, pty *app.PtyTerminalsService, webhooks *app.WebhookService, transport TerminalTransport, enabled bool) *TerminalService {
	return &TerminalService{terminals: terminals, pty: pty, webhooks: webhooks, transport: transport, enabled: enabled}
}

// Enabled reports the experimental.terminal opt-in (ADR 0037). The frontend
// renders the way into terminal mode only when it is on; availability stays a
// separate axis, because an enabled-but-unavailable terminal explains itself
// inside the mode instead of hiding the way in.
func (s *TerminalService) Enabled(ctx context.Context) bool { return s.enabled }

// Available answers even when the loopback server is down, which is why it
// composes tmux availability with the transport's own reachability.
func (s *TerminalService) Available(ctx context.Context) TerminalAvailability {
	if !s.enabled {
		return TerminalAvailability{Reason: "Terminal mode is off. Turn it on in Settings ▸ System, then relaunch Hive."}
	}
	endpoint, endpointErr := s.Endpoint(ctx)
	pty := endpointErr == nil && endpoint.PtyWSURL != "" && s.pty.Available(ctx) == nil
	if err := s.terminals.Available(ctx); err != nil {
		return TerminalAvailability{Reason: reasonFor(err), PtyAvailable: pty}
	}
	if endpointErr != nil {
		return TerminalAvailability{Reason: reasonFor(endpointErr), PtyAvailable: pty}
	}
	return TerminalAvailability{Available: true, PtyAvailable: pty}
}

// Endpoint reports where the terminal server is reachable, or KindUnavailable
// while the loopback server is unbound or HTTP is disabled.
func (s *TerminalService) Endpoint(ctx context.Context) (TerminalEndpoint, error) {
	if s.transport.Token == "" || s.transport.StreamPath == "" {
		return TerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The terminal transport is not mounted in this build.")
	}
	running, port := s.webhooks.Endpoint(ctx)
	if !running {
		return TerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The local HTTP server is not running, so the terminal has nothing to connect to. Check http.enabled in settings.yaml.")
	}
	authority := net.JoinHostPort(s.webhooks.Host(), strconv.Itoa(port))
	endpoint := TerminalEndpoint{
		HTTPBaseURL: "http://" + authority,
		WSURL:       "ws://" + authority + s.transport.StreamPath,
		Token:       s.transport.Token,
	}
	if s.transport.PtyStreamPath != "" {
		endpoint.PtyWSURL = "ws://" + authority + s.transport.PtyStreamPath
	}
	return endpoint, nil
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
