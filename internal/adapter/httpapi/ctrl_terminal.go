package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web/extractors"
)

const (
	// TerminalPathPrefix is the control plane's path space. Everything under it
	// authenticates and answers CORS; nothing else on this API does.
	TerminalPathPrefix = "/api/terminal/"
	// TerminalStreamPath is where the data-plane WebSocket is mounted. It sits
	// under the prefix above but is not an operation: ServeMux's longest match
	// routes it to its own raw handler instead of through the errchain table.
	TerminalStreamPath = "/api/terminal/stream"

	terminalTokenBytes = 32
	maxTerminalName    = 200
)

// MintTerminalToken returns a fresh per-run bearer token for the terminal
// surface. It is minted in the composition root, held in memory, never logged
// and never persisted (ADR 0034).
func MintTerminalToken() (string, error) {
	raw := make([]byte, terminalTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mint terminal token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// terminalWindow carries tmux's size for the window, which is the size the
// frontend must render it at — the cols/rows it asked for are only a vote.
type terminalWindow struct {
	WindowID string `json:"windowId"`
	Name     string `json:"name"`
	Active   bool   `json:"active"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type terminalSlugRequest struct {
	Slug string `json:"slug"`
}

func (b terminalSlugRequest) Validate() error {
	return criterio.Run("slug", b.Slug, criterio.Required)
}

type terminalSizeRequest struct {
	Slug string `json:"slug"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (b terminalSizeRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("slug", b.Slug, criterio.Required),
		criterio.Run("cols", b.Cols, criterio.Positive[int]()),
		criterio.Run("rows", b.Rows, criterio.Positive[int]()),
	)
}

type terminalWindowRequest struct {
	Slug     string `json:"slug"`
	WindowID string `json:"windowId"`
}

func (b terminalWindowRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("slug", b.Slug, criterio.Required),
		criterio.Run("windowId", b.WindowID, criterio.Required),
	)
}

type terminalRenameRequest struct {
	Slug     string `json:"slug"`
	WindowID string `json:"windowId"`
	Name     string `json:"name"`
}

func (b terminalRenameRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("slug", b.Slug, criterio.Required),
		criterio.Run("windowId", b.WindowID, criterio.Required),
		criterio.Run("name", b.Name, criterio.Required, criterio.StrMax(maxTerminalName)),
	)
}

type terminalAttachResponse struct {
	Windows    []terminalWindow `json:"windows"`
	StreamPath string           `json:"streamPath"`
}

type terminalNewWindowResponse struct {
	WindowID string `json:"windowId"`
}

// TerminalAttach opens the control client for a session slug and returns its
// windows plus the path the data-plane WebSocket is served on.
func (ctrl *Controller) TerminalAttach(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSizeRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	windows, err := ctrl.core.Terminals.Attach(r.Context(), body.Slug, body.Cols, body.Rows)
	if err != nil {
		return err
	}
	out := make([]terminalWindow, 0, len(windows))
	for _, win := range windows {
		out = append(out, terminalWindow{
			WindowID: win.ID,
			Name:     win.Name,
			Active:   win.Active,
			Width:    win.Width,
			Height:   win.Height,
		})
	}
	return server.JSON(w, http.StatusOK, terminalAttachResponse{Windows: out, StreamPath: TerminalStreamPath})
}

// TerminalResize renegotiates the control client's size.
func (ctrl *Controller) TerminalResize(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSizeRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Terminals.Resize(r.Context(), body.Slug, body.Cols, body.Rows); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// TerminalNewWindow creates a window and returns its id.
func (ctrl *Controller) TerminalNewWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	id, err := ctrl.core.Terminals.NewWindow(r.Context(), body.Slug)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalNewWindowResponse{WindowID: id})
}

// TerminalCloseWindow kills one window of an attached session.
func (ctrl *Controller) TerminalCloseWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalWindowRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Terminals.CloseWindow(r.Context(), body.Slug, body.WindowID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// TerminalRenameWindow renames one window of an attached session.
func (ctrl *Controller) TerminalRenameWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalRenameRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Terminals.RenameWindow(r.Context(), body.Slug, body.WindowID, body.Name); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// TerminalSelectWindow makes one window the session's active window.
func (ctrl *Controller) TerminalSelectWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalWindowRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Terminals.SelectWindow(r.Context(), body.Slug, body.WindowID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// TerminalDetach closes the control client, leaving the tmux session running.
func (ctrl *Controller) TerminalDetach(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Terminals.Detach(r.Context(), body.Slug); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// terminalBody enforces the bearer token before decoding, so an unauthenticated
// caller learns nothing about the body shape.
func terminalBody[T any](ctrl *Controller, w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	if err := requireTerminalToken(r, ctrl.terminalToken); err != nil {
		return zero, err
	}
	return extractors.Body[T](w, r)
}

// requireTerminalToken checks Authorization: Bearer against the per-run token.
// The terminal endpoints authenticate while their siblings deliberately do not:
// a terminal is arbitrary command execution, which no other route on this
// surface offers (ADR 0034).
func requireTerminalToken(r *http.Request, token string) error {
	if token == "" {
		return app.Errorf(app.KindUnavailable, "the terminal control plane is not configured")
	}
	header := r.Header.Get("Authorization")
	presented, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(token)) != 1 {
		return app.Errorf(app.KindUnauthenticated, "a terminal bearer token is required")
	}
	return nil
}

// corsPolicy answers preflights and stamps responses for the terminal paths.
// Auth is a bearer token rather than a cookie, so the allowlist does not have
// to be credentialed.
type corsPolicy struct {
	origins []string
	log     zerolog.Logger
}

func (c corsPolicy) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.stamp(w, r.Header.Get("Origin"))
		next(w, r)
	}
}

func (c corsPolicy) preflight(w http.ResponseWriter, r *http.Request) {
	if !c.stamp(w, r.Header.Get("Origin")) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c corsPolicy) stamp(w http.ResponseWriter, origin string) bool {
	w.Header().Add("Vary", "Origin")
	if origin == "" || !originAllowed(c.origins, origin) {
		if origin != "" {
			c.log.Debug().Str("origin", origin).Strs("allowed", c.origins).Msg("terminal origin rejected")
		}
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	return true
}

// originAllowed matches an Origin header against the allowlist composed in
// main.go. An absent Origin is not this function's call: a non-browser client
// sends none, and the bearer token is what authenticates it either way.
//
// The wails:// scheme is accepted structurally rather than by list: only this
// app's own webview can produce it (a web page's origin is always a web
// scheme), and macOS appends the dev server's freshly-picked port to it every
// dev run, so no static list can name it.
func originAllowed(allowed []string, origin string) bool {
	if origin == "wails://localhost" || strings.HasPrefix(origin, "wails://localhost:") {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(candidate, origin) {
			return true
		}
	}
	return false
}
