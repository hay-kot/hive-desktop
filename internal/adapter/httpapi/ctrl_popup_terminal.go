package httpapi

import (
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// The pop-up surface sits under TerminalPathPrefix so the bearer token and the
// CORS policy that guard the terminal control plane cover it without a second
// rule — a PTY is arbitrary command execution just as tmux is (ADR 0036). What
// is behind it is not a tmux session and is addressed by id rather than by
// slug, so it is its own path space rather than more verbs on the tmux one.
const (
	PopupTerminalPathPrefix = "/api/terminal/popup/"

	maxPopupCommand = 2000
	maxPopupDir     = 4096
)

type popupOpenRequest struct {
	// Launcher names a configured launcher to open, supplying the command and —
	// when it configures one — the directory. Sending the id rather than the
	// command is what keeps a launcher's definition the catalog's answer.
	Launcher string `json:"launcher"`
	// SessionSlug opens the terminal in that hive session's checkout. It wins
	// over Dir; with neither, the terminal opens in the user's home directory.
	SessionSlug string `json:"sessionSlug"`
	Dir         string `json:"dir"`
	// Command is a shell command line — it is run through a login shell, so what
	// a user would type in their own terminal is what runs. Empty opens an
	// interactive shell.
	Command string `json:"command"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}

func (b popupOpenRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("dir", b.Dir, criterio.StrMax(maxPopupDir)),
		criterio.Run("command", b.Command, criterio.StrMax(maxPopupCommand)),
	)
}

// popupLauncher is one configured launcher. What it runs is absent by design:
// a caller opens it by id.
type popupLauncher struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

type popupLauncherListResponse struct {
	Launchers []popupLauncher `json:"launchers"`
}

type popupIDRequest struct {
	ID string `json:"id"`
}

func (b popupIDRequest) Validate() error {
	return criterio.Run("id", b.ID, criterio.Required)
}

type popupSizeRequest struct {
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (b popupSizeRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("id", b.ID, criterio.Required),
		criterio.Run("cols", b.Cols, criterio.Positive[int]()),
		criterio.Run("rows", b.Rows, criterio.Positive[int]()),
	)
}

// popupTerminal is one open terminal. The id is this process's own and means
// nothing after it exits, which is also the whole lifetime of the terminal.
type popupTerminal struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Command string `json:"command"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}

type popupListResponse struct {
	Terminals []popupTerminal `json:"terminals"`
}

// popupCloseResponse reports whether there was a terminal to close.
type popupCloseResponse struct {
	Closed bool `json:"closed"`
}

func toPopupTerminal(term ptyterm.Terminal) popupTerminal {
	return popupTerminal{
		ID:      term.ID,
		Title:   term.Title,
		Dir:     term.Dir,
		Command: term.Command,
		Cols:    term.Cols,
		Rows:    term.Rows,
	}
}

// PopupTerminalOpen launches a terminal and returns it.
func (ctrl *Controller) PopupTerminalOpen(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[popupOpenRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	term, err := ctrl.core.PopupTerminals.Open(r.Context(), app.OpenPopupTerminal{
		Launcher:    body.Launcher,
		SessionSlug: body.SessionSlug,
		Dir:         body.Dir,
		Command:     body.Command,
		Cols:        body.Cols,
		Rows:        body.Rows,
	})
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, toPopupTerminal(term))
}

// PopupTerminalLaunchers answers the configured launchers, in catalog order.
func (ctrl *Controller) PopupTerminalLaunchers(w http.ResponseWriter, r *http.Request) error {
	if _, err := terminalBody[struct{}](ctrl, w, r); err != nil {
		return err
	}
	launchers, err := ctrl.core.PopupTerminals.Launchers(r.Context())
	if err != nil {
		return err
	}
	out := make([]popupLauncher, 0, len(launchers))
	for _, l := range launchers {
		out = append(out, popupLauncher{ID: l.ID, Label: l.Label, Icon: l.Icon})
	}
	return server.JSON(w, http.StatusOK, popupLauncherListResponse{Launchers: out})
}

// PopupTerminalClose ends a terminal and every process in it.
func (ctrl *Controller) PopupTerminalClose(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[popupIDRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	closed, err := ctrl.core.PopupTerminals.Close(r.Context(), body.ID)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, popupCloseResponse{Closed: closed})
}

// PopupTerminalList answers the open terminals, oldest first.
func (ctrl *Controller) PopupTerminalList(w http.ResponseWriter, r *http.Request) error {
	if _, err := terminalBody[struct{}](ctrl, w, r); err != nil {
		return err
	}
	terminals, err := ctrl.core.PopupTerminals.List(r.Context())
	if err != nil {
		return err
	}
	out := make([]popupTerminal, 0, len(terminals))
	for _, term := range terminals {
		out = append(out, toPopupTerminal(term))
	}
	return server.JSON(w, http.StatusOK, popupListResponse{Terminals: out})
}

// PopupTerminalResize sets the terminal's size. Nothing else is attached to
// these PTYs, so this is applied rather than voted on.
func (ctrl *Controller) PopupTerminalResize(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[popupSizeRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.PopupTerminals.Resize(r.Context(), body.ID, body.Cols, body.Rows); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
