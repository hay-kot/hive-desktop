package httpapi

import (
	"net/http"

	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// PtyTerminalPathPrefix is the process-managed backend's path space. It sits
// under TerminalPathPrefix so the bearer token and the CORS policy that guard
// the terminal surface cover it without a second rule: it is the same surface,
// with a different thing behind it (ADR 0045).
const (
	PtyTerminalPathPrefix = "/api/terminal/pty/"
	PtyTerminalStreamPath = "/api/terminal/pty/stream"
)

// The request and response bodies are deliberately the tmux backend's. Two
// wire shapes for one surface would let the frontend's two clients drift, and
// the whole point of the comparison is that only the engine differs.

func toPtyTerminalWindows(windows []ptyterm.Window) []terminalWindow {
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
	return out
}

// PtyTerminalAttach returns the windows of a live process-managed session.
func (ctrl *Controller) PtyTerminalAttach(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalAttachRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	windows, err := ctrl.core.PtyTerminals.Attach(r.Context(), body.Slug, body.Cols, body.Rows)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalAttachResponse{Windows: toPtyTerminalWindows(windows)})
}

// PtyTerminalStart opens a session with one shell in the hive session's checkout.
func (ctrl *Controller) PtyTerminalStart(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	started, err := ctrl.core.PtyTerminals.Start(r.Context(), body.Slug)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalStartResponse{Started: started})
}

// PtyTerminalKill ends the session and every process in it.
func (ctrl *Controller) PtyTerminalKill(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	killed, err := ctrl.core.PtyTerminals.Kill(r.Context(), body.Slug)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalKillResponse{Killed: killed})
}

// PtyTerminalListWindows answers a slug's windows without attaching.
func (ctrl *Controller) PtyTerminalListWindows(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	windows, err := ctrl.core.PtyTerminals.ListWindows(r.Context(), body.Slug)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalWindowsResponse{Windows: toPtyTerminalWindows(windows)})
}

// PtyTerminalResize sets the session's size. Nothing else is attached to these
// PTYs, so this is applied rather than voted on.
func (ctrl *Controller) PtyTerminalResize(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSizeRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PtyTerminals.Resize(r.Context(), body.Slug, body.Cols, body.Rows); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PtyTerminalNewWindow spawns another shell in the session and returns its id.
func (ctrl *Controller) PtyTerminalNewWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	id, err := ctrl.core.PtyTerminals.NewWindow(r.Context(), body.Slug)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, terminalNewWindowResponse{WindowID: id})
}

// PtyTerminalCloseWindow hangs up one window of the session.
func (ctrl *Controller) PtyTerminalCloseWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalWindowRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PtyTerminals.CloseWindow(r.Context(), body.Slug, body.WindowID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PtyTerminalRenameWindow renames one window. The name is this app's own label:
// nothing outside the process knows it, and no program inside can set it.
func (ctrl *Controller) PtyTerminalRenameWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalRenameRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PtyTerminals.RenameWindow(r.Context(), body.Slug, body.WindowID, body.Name); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PtyTerminalSelectWindow makes one window the session's active window.
func (ctrl *Controller) PtyTerminalSelectWindow(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalWindowRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PtyTerminals.SelectWindow(r.Context(), body.Slug, body.WindowID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PtyTerminalDetach releases nothing; it answers the transport's vocabulary so
// one frontend client interface serves both backends.
func (ctrl *Controller) PtyTerminalDetach(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[terminalSlugRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PtyTerminals.Detach(r.Context(), body.Slug); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
