package httpapi

import (
	"errors"
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// The agent-workspace control plane sits under TerminalPathPrefix for exactly
// the reason the pop-up surface does: starting a session spawns an agent CLI,
// which is arbitrary command execution just as a pop-up shell is (ADR 0036),
// and mounting it here is what gives it the terminal bearer token and CORS
// policy without a second rule. Sessions are tmux sessions named
// agentws-<record id> and ride the shared tmux data plane at
// TerminalStreamPath (ADR 0063) — there is no agent-specific stream. Start and
// Resume attach server-side and return the active window id alongside the
// session name, because the generic /api/terminal/attach control route is
// gated by experimental.terminal, which the Agents area must not depend on.
const AgentWorkspacesPathPrefix = TerminalPathPrefix + "agents/"

// agentWorkspaceView is one row of the area's workspace list. Autonomy and
// MCPs are on it because a workspace that can actuate the physical world says
// so where it is opened, not where it was configured (spec §7.2, ADR 0061).
type agentWorkspaceView struct {
	Dir      string   `json:"dir"`
	Name     string   `json:"name"`
	Agent    string   `json:"agent"`
	Autonomy string   `json:"autonomy"`
	MCPs     []string `json:"mcps"`
	Problem  string   `json:"problem"`
	// Notice explains an agent whose MCP wiring cannot bound its tool set to
	// the workspace's declared servers, empty when the wiring is bounded or
	// the workspace's manifest failed to parse.
	Notice string `json:"notice"`
}

// agentSessionView is one row of a workspace's session list.
type agentSessionView struct {
	ID           int64  `json:"id"`
	Workspace    string `json:"workspace"`
	Name         string `json:"name"`
	Agent        string `json:"agent"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
	// TerminalID is the tmux session name (agentws-<id>) addressed on
	// TerminalStreamPath, empty when nothing is running.
	TerminalID string `json:"terminalId"`
	// WindowID is TerminalID's active tmux window, needed to frame input and
	// output on the windowed tmux wire. Set only by Start/Resume, which
	// attach; a listing read leaves it empty even for a live session.
	WindowID string `json:"windowId"`
	// ResumeAttempted is false when this launch could not even try to resume —
	// the agent has no resume form.
	ResumeAttempted bool `json:"resumeAttempted"`
	// Notice carries a fresh-launch, unbounded-MCP, or missing-MCP explanation
	// for the UI to show beside the session.
	Notice string `json:"notice"`
}

func toAgentWorkspaceView(w app.WorkspaceView) agentWorkspaceView {
	return agentWorkspaceView{
		Dir: w.Dir, Name: w.Name, Agent: w.Agent, Autonomy: w.Autonomy, MCPs: nonNilStrings(w.MCPs), Problem: w.Problem,
		Notice: w.Notice,
	}
}

// nonNilStrings normalizes nil to an empty, non-nil slice. encoding/json
// marshals a nil slice as the JSON literal null rather than [], and the
// frontend calls .length on every array field unconditionally — a workspace
// with no mcps: entries, or an open with nothing missing, must still answer
// with an empty array on the wire, not null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func toAgentWorkspaceViews(in []app.WorkspaceView) []agentWorkspaceView {
	out := make([]agentWorkspaceView, 0, len(in))
	for _, w := range in {
		out = append(out, toAgentWorkspaceView(w))
	}
	return out
}

func toAgentSessionView(s app.SessionView) agentSessionView {
	return agentSessionView{
		ID: s.ID, Workspace: s.Workspace, Name: s.Name, Agent: s.Agent, LastOpenedAt: s.LastOpenedAt,
		TerminalID: s.TerminalID, WindowID: s.WindowID, ResumeAttempted: s.ResumeAttempted, Notice: s.Notice,
	}
}

func toAgentSessionViews(in []app.SessionView) []agentSessionView {
	out := make([]agentSessionView, 0, len(in))
	for _, s := range in {
		out = append(out, toAgentSessionView(s))
	}
	return out
}

// agentWorkspacesResponse is AgentWorkspaces' payload. Available/Error report
// whether ephemeral terminals can run at all in this build (the same axis
// PopupTerminalAvailability reports); RootProblem is a distinct axis — the
// configured root itself could not be created or opened at startup (spec
// §14, e.g. an unmounted volume or a signed-out iCloud Drive). Root is the
// configured path regardless of either answer, so the UI can point at the
// setting even when nothing can run.
type agentWorkspacesResponse struct {
	Root        string               `json:"root"`
	RootProblem string               `json:"rootProblem"`
	Available   bool                 `json:"available"`
	Error       string               `json:"error"`
	Workspaces  []agentWorkspaceView `json:"workspaces"`
}

// AgentWorkspaces lists every recognized workspace under the configured root.
func (ctrl *Controller) AgentWorkspaces(w http.ResponseWriter, r *http.Request) error {
	if _, err := terminalBody[struct{}](ctrl, w, r); err != nil {
		return err
	}
	workspaces, err := ctrl.core.AgentWorkspaces.List(r.Context())
	if err != nil {
		return err
	}
	available, errMsg := true, ""
	if avErr := ctrl.core.AgentWorkspaces.Available(r.Context()); avErr != nil {
		available, errMsg = false, agentErrorMessage(avErr)
	}
	return server.JSON(w, http.StatusOK, agentWorkspacesResponse{
		Root:        ctrl.core.RuntimePaths().AgentWorkspacesDir,
		RootProblem: ctrl.core.AgentWorkspaces.RootProblem(r.Context()),
		Available:   available,
		Error:       errMsg,
		Workspaces:  toAgentWorkspaceViews(workspaces),
	})
}

type agentWorkspaceOpenRequest struct {
	Dir string `json:"dir"`
}

func (b agentWorkspaceOpenRequest) Validate() error {
	return criterio.Run("dir", b.Dir, criterio.Required)
}

type agentWorkspaceOpenResponse struct {
	Workspace agentWorkspaceView `json:"workspace"`
	Sessions  []agentSessionView `json:"sessions"`
	// MissingMCPs names ids the workspace declares that the catalogue does
	// not resolve; they are simply omitted from what was generated.
	MissingMCPs []string `json:"missingMcps"`
}

// AgentWorkspaceOpen regenerates a workspace's disposable artifacts and
// returns its sessions. It is the only call that writes into a workspace.
func (ctrl *Controller) AgentWorkspaceOpen(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentWorkspaceOpenRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	result, err := ctrl.core.AgentWorkspaces.Open(r.Context(), body.Dir)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentWorkspaceOpenResponse{
		Workspace:   toAgentWorkspaceView(result.Workspace),
		Sessions:    toAgentSessionViews(result.Sessions),
		MissingMCPs: nonNilStrings(result.MissingMCPs),
	})
}

type agentWorkspaceDeleteRequest struct {
	Dir string `json:"dir"`
}

func (b agentWorkspaceDeleteRequest) Validate() error {
	return criterio.Run("dir", b.Dir, criterio.Required)
}

// AgentWorkspaceDelete ends every live terminal a workspace's sessions hold
// and removes their records. The workspace directory itself is untouched —
// it is the user's, and possibly under version control (spec §14).
func (ctrl *Controller) AgentWorkspaceDelete(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentWorkspaceDeleteRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.AgentWorkspaces.DeleteWorkspace(r.Context(), body.Dir); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type agentSessionsRequest struct {
	Workspace string `json:"workspace"`
}

func (b agentSessionsRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentSessionsResponse struct {
	Sessions []agentSessionView `json:"sessions"`
}

// AgentSessions lists a workspace's sessions without regenerating its
// artifacts, unlike AgentWorkspaceOpen.
func (ctrl *Controller) AgentSessions(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionsRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	sessions, err := ctrl.core.AgentWorkspaces.Sessions(r.Context(), body.Workspace)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentSessionsResponse{Sessions: toAgentSessionViews(sessions)})
}

// agentSessionStartRequest's Cols/Rows carry no Validate: 0x0 opens at the
// terminal's own default, the same convention popupOpenRequest follows.
type agentSessionStartRequest struct {
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

func (b agentSessionStartRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("workspace", b.Workspace, criterio.Required),
		criterio.Run("name", b.Name, criterio.Required),
	)
}

// AgentSessionStart launches a new, named session in a workspace: resolving
// its agent, autonomy posture and MCP wiring into a command line and opening
// it on a Hive-owned PTY.
func (ctrl *Controller) AgentSessionStart(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionStartRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	view, err := ctrl.core.AgentWorkspaces.StartSession(r.Context(), app.StartSession{
		Workspace: body.Workspace, Name: body.Name, Cols: body.Cols, Rows: body.Rows,
	})
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, toAgentSessionView(view))
}

type agentSessionResumeRequest struct {
	ID   int64 `json:"id"`
	Cols int   `json:"cols"`
	Rows int   `json:"rows"`
}

func (b agentSessionResumeRequest) Validate() error {
	return criterio.Run("id", b.ID, criterio.Positive[int64]())
}

// AgentSessionResume reattaches a session's live terminal if it still has
// one, or relaunches it — resuming the agent's own conversation when it has a
// resume form (resumeAttempted), and starting fresh with a notice when it
// does not (spec §6.2).
func (ctrl *Controller) AgentSessionResume(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionResumeRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	view, err := ctrl.core.AgentWorkspaces.ResumeSession(r.Context(), body.ID, body.Cols, body.Rows)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, toAgentSessionView(view))
}

type agentSessionIDRequest struct {
	ID int64 `json:"id"`
}

func (b agentSessionIDRequest) Validate() error {
	return criterio.Run("id", b.ID, criterio.Positive[int64]())
}

type agentSessionCloseResponse struct {
	Closed bool `json:"closed"`
}

// AgentSessionClose ends a session's live terminal and reports whether there
// was one to close. The record is untouched, so it still lists afterward.
func (ctrl *Controller) AgentSessionClose(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionIDRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	closed, err := ctrl.core.AgentWorkspaces.CloseSession(r.Context(), body.ID)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentSessionCloseResponse{Closed: closed})
}

// agentSessionActivityRequest scopes activity classification to one
// workspace, matching AgentSessions — the frontend polls whichever
// workspace's session list is on screen.
type agentSessionActivityRequest struct {
	Workspace string `json:"workspace"`
}

func (b agentSessionActivityRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentSessionActivityItem struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type agentSessionActivityResponse struct {
	Items []agentSessionActivityItem `json:"items"`
}

// AgentSessionActivity classifies each of a workspace's live sessions from
// its captured tmux pane: ready, active, or approval — approval is the
// highest-urgency state. A session with no live tmux session is omitted
// rather than reported dead; its row's terminalId already carries that.
func (ctrl *Controller) AgentSessionActivity(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionActivityRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	items, err := ctrl.core.AgentWorkspaces.SessionActivity(r.Context(), body.Workspace)
	if err != nil {
		return err
	}
	out := make([]agentSessionActivityItem, 0, len(items))
	for _, it := range items {
		out = append(out, agentSessionActivityItem{ID: it.ID, Status: it.Status})
	}
	return server.JSON(w, http.StatusOK, agentSessionActivityResponse{Items: out})
}

// AgentSessionDelete ends any live terminal and deletes a session's record.
func (ctrl *Controller) AgentSessionDelete(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSessionIDRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.AgentWorkspaces.DeleteSession(r.Context(), body.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// agentErrorMessage takes the user-facing message off a core error, matching
// wailsui's reasonFor: anything not an *app.Error is reported verbatim.
func agentErrorMessage(err error) string {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		return appErr.Msg
	}
	return err.Error()
}
