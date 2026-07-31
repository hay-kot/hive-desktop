package wailsui

import (
	"context"
	"net"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// PopupTerminalTransport is what the pop-up needs that the core does not hold:
// the per-run bearer token and the path its WebSocket is mounted at. Both are
// composed in main.go and handed here (ADR 0036). It shares the terminal's
// token because it shares the terminal's path prefix.
type PopupTerminalTransport struct {
	Token      string
	StreamPath string
}

// PopupTerminalAvailability gates the pop-up terminal. Unlike terminal mode it
// does not depend on tmux — there is no program to discover — so a machine
// without one still gets a pop-up shell.
type PopupTerminalAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// PopupTerminalEndpoint bootstraps the webview: control actions go to
// HTTPBaseURL with the bearer token, the data plane opens WSURL.
type PopupTerminalEndpoint struct {
	HTTPBaseURL string `json:"httpBaseURL"`
	WSURL       string `json:"wsURL"`
	Token       string `json:"token"`
}

// PopupTerminalService is the pop-up's bootstrap and nothing else: opening,
// closing and streaming a terminal all go over the loopback HTTP surface, so
// neither the control plane nor the data plane crosses the Wails bridge.
type PopupTerminalService struct {
	terminals *app.PopupTerminalsService
	webhooks  *app.WebhookService
	transport PopupTerminalTransport
	enabled   bool
}

func NewPopupTerminalService(terminals *app.PopupTerminalsService, webhooks *app.WebhookService, transport PopupTerminalTransport, enabled bool) *PopupTerminalService {
	return &PopupTerminalService{terminals: terminals, webhooks: webhooks, transport: transport, enabled: enabled}
}

// Available reports whether a pop-up terminal can be opened, with a reason the
// frontend renders as-is when it cannot.
func (s *PopupTerminalService) Available(ctx context.Context) PopupTerminalAvailability {
	if !s.enabled {
		return PopupTerminalAvailability{Reason: "Terminal features are off. Turn them on in Settings ▸ System, then relaunch Hive."}
	}
	if err := s.terminals.Available(ctx); err != nil {
		return PopupTerminalAvailability{Reason: reasonFor(err)}
	}
	if _, err := s.Endpoint(ctx); err != nil {
		return PopupTerminalAvailability{Reason: reasonFor(err)}
	}
	return PopupTerminalAvailability{Available: true}
}

// Endpoint reports where the pop-up's surface is reachable, or KindUnavailable
// when this run has no loopback server or no token to reach it with.
func (s *PopupTerminalService) Endpoint(ctx context.Context) (PopupTerminalEndpoint, error) {
	if s.transport.Token == "" || s.transport.StreamPath == "" {
		return PopupTerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The pop-up terminal transport was not mounted for this run.")
	}
	running, port := s.webhooks.Endpoint(ctx)
	if !running {
		return PopupTerminalEndpoint{}, app.Errorf(app.KindUnavailable, "The local HTTP server is not running, so the terminal has nothing to connect to. Check http.enabled in settings.yaml.")
	}
	authority := net.JoinHostPort(s.webhooks.Host(), strconv.Itoa(port))
	return PopupTerminalEndpoint{
		HTTPBaseURL: "http://" + authority,
		WSURL:       "ws://" + authority + s.transport.StreamPath,
		Token:       s.transport.Token,
	}, nil
}
