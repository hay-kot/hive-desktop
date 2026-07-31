package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// PtyTerminalStreamHandler returns the mount prefix and the raw handler for the
// process-managed data plane. It carries the frames terminal_stream.go defines,
// byte for byte: the renderer on the far end must not be able to tell which
// backend is behind the socket, or the comparison measures the transport rather
// than the engine (ADR 0045).
func PtyTerminalStreamHandler(core *app.App, token string, origins []string, log zerolog.Logger) (string, http.Handler) {
	return PtyTerminalStreamPath, &ptyTerminalStream{core: core, token: token, origins: origins, log: log}
}

type ptyTerminalStream struct {
	core    *app.App
	token   string
	origins []string
	log     zerolog.Logger
}

func (h *ptyTerminalStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("v") != terminalWireVersion {
		http.Error(w, "unsupported terminal wire version", http.StatusBadRequest)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(h.origins, origin) {
		h.log.Debug().Str("origin", origin).Strs("allowed", h.origins).Msg("pty terminal stream origin rejected")
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}

	presented, fromSubprotocol := streamToken(r)
	if h.token == "" || presented != h.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slug := query.Get("slug")
	events, unsubscribe, err := h.core.PtyTerminals.Subscribe(r.Context(), slug)
	if err != nil {
		status := statusForKind(app.KindOf(err))
		http.Error(w, http.StatusText(status), status)
		return
	}

	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	}
	if fromSubprotocol {
		opts.Subprotocols = []string{presented}
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		unsubscribe()
		h.log.Debug().Err(err).Str("session", slug).Msg("pty terminal websocket upgrade failed")
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxInputFrameBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		h.writePump(ctx, conn, slug, events)
	}()

	h.readPump(ctx, conn, slug)
	cancel()
	unsubscribe()
	<-writeDone
}

func (h *ptyTerminalStream) writePump(ctx context.Context, conn *websocket.Conn, slug string, events <-chan ptyterm.Event) {
	for ev := range events {
		frame, ok := encodePtyEvent(ev)
		if !ok {
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, streamWriteTimeout)
		err := conn.Write(writeCtx, websocket.MessageBinary, frame)
		cancel()
		if err != nil {
			return
		}
		if out, isOutput := ev.(ptyterm.Output); isOutput {
			h.core.PtyTerminals.ObserveFrameLatency(slug, out.WindowID, time.Since(out.At))
		}
	}
	_ = conn.Close(websocket.StatusNormalClosure, "terminal session ended")
}

func (h *ptyTerminalStream) readPump(ctx context.Context, conn *websocket.Conn, slug string) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageBinary {
			_ = conn.Close(websocket.StatusUnsupportedData, "binary frames only")
			return
		}
		windowID, payload, err := decodeInputFrame(data)
		if err != nil {
			_ = conn.Close(websocket.StatusInvalidFramePayloadData, "malformed input frame")
			return
		}
		if err := h.core.PtyTerminals.Write(ctx, slug, windowID, payload); err != nil {
			h.log.Debug().Err(err).Str("session", slug).Str("window", windowID).Msg("pty terminal input rejected")
		}
	}
}

// encodePtyEvent is the tmux encoder's counterpart. A PTY has no panes, so the
// window id stands in for the pane id the frame carries — the field exists for
// tmux's deferred panes phase and the renderer only needs it to be stable.
func encodePtyEvent(ev ptyterm.Event) ([]byte, bool) {
	switch v := ev.(type) {
	case ptyterm.Output:
		return encodeOutputFrame(v.WindowID, v.WindowID, v.Data), true
	case ptyterm.WindowChanged:
		return encodeJSONFrame(frameWindowEvent, windowEventPayload{
			Kind:     string(v.Kind),
			WindowID: v.Window.ID,
			Name:     v.Window.Name,
			Active:   v.Window.Active,
			Width:    v.Window.Width,
			Height:   v.Window.Height,
		})
	case ptyterm.LifecycleChanged:
		return encodeJSONFrame(frameLifecycle, lifecyclePayload{
			Kind:     string(v.Kind),
			WindowID: v.WindowID,
			Message:  v.Message,
		})
	default:
		return nil, false
	}
}
