package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// The pop-up data plane. One socket carries exactly one terminal — the id is in
// the query string, not in every frame — so output is a byte payload behind a
// single tag and xterm.js writes it without a decode step.
//
//	server -> client
//	  0x00 Output [0x00][raw bytes]
//	  0x01 Exit   [0x01][JSON {reason}]
//	client -> server
//	  0x10 Input  [0x10][raw bytes]
const (
	popupFrameOutput byte = 0x00
	popupFrameExit   byte = 0x01
	popupFrameInput  byte = 0x10
)

// PopupTerminalStreamHandler returns the mount path and the raw handler for the
// pop-up data plane. Like the tmux stream it is mounted on its own rather than
// joining the operations table: the errchain cannot frame a hijacked socket.
func PopupTerminalStreamHandler(core *app.App, token string, origins []string, log zerolog.Logger) (string, http.Handler) {
	return PopupTerminalStreamPath, &popupTerminalStream{core: core, token: token, origins: origins, log: log}
}

type popupTerminalStream struct {
	core    *app.App
	token   string
	origins []string
	log     zerolog.Logger
}

type popupExitPayload struct {
	Reason string `json:"reason"`
}

func (h *popupTerminalStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("v") != terminalWireVersion {
		http.Error(w, "unsupported terminal wire version", http.StatusBadRequest)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(h.origins, origin) {
		h.log.Debug().Str("origin", origin).Strs("allowed", h.origins).Msg("popup terminal stream origin rejected")
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}

	presented, fromSubprotocol := streamToken(r)
	if h.token == "" || presented != h.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := query.Get("id")
	events, unsubscribe, err := h.core.PopupTerminals.Subscribe(r.Context(), id)
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
		h.log.Debug().Err(err).Str("terminal", id).Msg("popup terminal websocket upgrade failed")
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxInputFrameBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		h.writePump(ctx, conn, events)
	}()

	h.readPump(ctx, conn, id)
	cancel()
	unsubscribe()
	<-writeDone
}

func (h *popupTerminalStream) writePump(ctx context.Context, conn *websocket.Conn, events <-chan ptyterm.Event) {
	for ev := range events {
		frame, ok := encodePopupEvent(ev)
		if !ok {
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, streamWriteTimeout)
		err := conn.Write(writeCtx, websocket.MessageBinary, frame)
		cancel()
		if err != nil {
			return
		}
	}
	_ = conn.Close(websocket.StatusNormalClosure, "terminal ended")
}

func (h *popupTerminalStream) readPump(ctx context.Context, conn *websocket.Conn, id string) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageBinary {
			_ = conn.Close(websocket.StatusUnsupportedData, "binary frames only")
			return
		}
		payload, err := decodePopupInputFrame(data)
		if err != nil {
			_ = conn.Close(websocket.StatusInvalidFramePayloadData, "malformed input frame")
			return
		}
		if err := h.core.PopupTerminals.Write(ctx, id, payload); err != nil {
			h.log.Debug().Err(err).Str("terminal", id).Msg("popup terminal input rejected")
		}
	}
}

func encodePopupEvent(ev ptyterm.Event) ([]byte, bool) {
	switch v := ev.(type) {
	case ptyterm.Output:
		return append([]byte{popupFrameOutput}, v.Data...), true
	case ptyterm.Exited:
		return encodeJSONFrame(popupFrameExit, popupExitPayload{Reason: v.Reason})
	default:
		return nil, false
	}
}

func decodePopupInputFrame(frame []byte) ([]byte, error) {
	if len(frame) == 0 || frame[0] != popupFrameInput {
		return nil, errors.New("not an input frame")
	}
	return frame[1:], nil
}
