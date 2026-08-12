package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hay-kot/httpkit/errchain"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web"
	"github.com/hay-kot/hive-desktop/internal/web/mid"
)

// Op is one HTTP operation. The operations table is what the mux is built
// from, and Summary/Request/Response/Errors document each route where it is
// declared. They no longer feed a generated document: the agent-facing surface
// this adapter used to describe is the MCP server's now (ADR mcp-replaces-the-agent-facing-http-api), and what
// is left here is the Wails frontend's own transport plus the liveness probe —
// consumers that read hand-written clients, not an OpenAPI spec.
type Op struct {
	Method   string
	Path     string
	Summary  string
	Request  any
	Response any
	Status   int       // success status; 0 means 200
	Errors   []ErrResp // documented non-success statuses beyond the generic default
	Handler  errchain.HandlerFunc
}

// ErrResp documents one non-success status an operation can return, and when.
// The error body is always the shared {kind, message, fields?} shape.
type ErrResp struct {
	Status int
	When   string
}

// pattern is the ServeMux pattern this op registers under. A path ending in "/"
// is anchored with {$} so it matches only that exact path rather than becoming
// a subtree that swallows every otherwise-unmatched /api/… request instead of
// 404ing.
func (op Op) pattern() string {
	path := op.Path
	if strings.HasSuffix(path, "/") {
		path += "{$}"
	}
	return op.Method + " " + path
}

func (ctrl *Controller) operations() []Op {
	ops := ctrl.baseOperations()
	ops = append(ops, ctrl.terminalOperations()...)
	ops = append(ops, ctrl.popupTerminalOperations()...)
	ops = append(ops, ctrl.agentOperations()...)
	return ops
}

// popupTerminalOperations is the ephemeral surface: terminals this process owns
// outright, opened on demand and addressed by an id it mints (ADR ephemeral-popup-terminals). They
// ride the terminal bearer token and CORS policy by sitting under its prefix.
func (ctrl *Controller) popupTerminalOperations() []Op {
	return []Op{
		{
			Method: "POST", Path: PopupTerminalPathPrefix + "open", Summary: "Open an ephemeral terminal and return it. The directory is resolved in order — the launcher's own cwd, sessionSlug's checkout, then dir (a leading ~ is expanded), then the user's home. command is a shell command line run through a login shell, so a user's aliases, functions and PATH resolve it; empty opens an interactive shell. launcher names a configured launcher (the launchers list in actions.yml) to open instead, and brings its own command. The terminal is this process's child: it has no name outside this run, nothing else can attach to it, and it ends when it is closed or when Hive exits. The data plane is a WebSocket served at " + PTYStreamPath + ", outside this operations table.",
			Request: popupOpenRequest{}, Response: popupTerminal{}, Handler: ctrl.PopupTerminalOpen,
			Errors: popupTerminalErrors("no hive session carries that slug, or no launcher carries that id",
				ErrResp{Status: 409, When: "the hive session is not active, so it has no checkout to open a terminal in"}),
		},
		{
			Method: "POST", Path: PopupTerminalPathPrefix + "close", Summary: "End a terminal and every process in it, and report whether there was one to close. An id whose process already exited answers closed=false rather than failing: an exited terminal is dropped, not kept.",
			Request: popupIDRequest{}, Response: popupCloseResponse{}, Handler: ctrl.PopupTerminalClose,
			Errors: popupTerminalErrors(""),
		},
		{
			Method: "POST", Path: PopupTerminalPathPrefix + "list", Summary: "List the open terminals, oldest first. Terminals whose process has exited are absent.",
			Response: popupListResponse{}, Handler: ctrl.PopupTerminalList,
			Errors: popupTerminalErrors(""),
		},
		{
			Method: "POST", Path: PopupTerminalPathPrefix + "resize", Summary: "Set a terminal's size. This is applied, not voted on: one client renders the PTY, so the size asked for is the size the process is told.",
			Request: popupSizeRequest{}, Status: http.StatusNoContent, Handler: ctrl.PopupTerminalResize,
			Errors: popupTerminalErrors("no open terminal carries that id"),
		},
	}
}

// popupTerminalErrors is terminalErrors with the unavailable reason that fits
// this surface: there is no program to install, so a 503 here is the build or
// the platform and nothing a user can act on.
func popupTerminalErrors(notFound string, extra ...ErrResp) []ErrResp {
	errs := []ErrResp{{Status: 401, When: "the Authorization: Bearer token is missing or wrong"}}
	if notFound != "" {
		errs = append(errs, ErrResp{Status: 404, When: notFound})
	}
	errs = append(errs, extra...)
	return append(errs, ErrResp{Status: 503, When: "ephemeral terminals are unavailable: an unsupported platform or a server build"})
}

// agentOperations is the agent-workspace control plane: named, durable
// workspaces where a CLI agent runs against a purpose-built MCP tool set
// (spec-tracked as hc-49x3i833). Like popupTerminalOperations it rides the
// terminal bearer token and CORS policy by sitting under TerminalPathPrefix —
// starting a session spawns an agent CLI, which is arbitrary command
// execution (ADR terminal-transport, ADR a-workspace-declares-its-own-authority).
func (ctrl *Controller) agentOperations() []Op {
	return []Op{
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces", Summary: "List every recognized agent workspace under the configured root, valid or not. A workspace whose manifest fails to parse still lists with its last-good name and agent, plus a problem explaining what is wrong. available/error report whether ephemeral terminals can run at all in this build; root is the configured workspace root regardless of that answer. autonomyFlags maps agent → posture → the CLI flags that posture launches with (a posture absent from an agent's map is refused at launch); editor names the configured open-in-editor command, empty when none is set.",
			Response: agentWorkspacesResponse{}, Handler: ctrl.AgentWorkspaces,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/open", Summary: "Regenerate a workspace's disposable artifacts (CLAUDE.md, .mcp.json, .codex/config.toml, .claude/, .agents/, an empty docs/) from its manifest and return its sessions. This is the only call that writes into a workspace; missingMcps names declared MCP ids the catalogue does not resolve, missingPackages skill packages skills.yml does not define.",
			Request: agentWorkspaceDirRequest{}, Response: agentWorkspaceOpenResponse{}, Handler: ctrl.AgentWorkspaceOpen,
			Errors: agentErrors("no such workspace, or its manifest is invalid"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/create", Summary: "Create a workspace: a new directory under the root with a fresh agent-workspace.yaml naming the given name, agent, and autonomy posture.",
			Request: agentWorkspaceEditRequest{}, Response: agentWorkspaceView{}, Handler: ctrl.AgentWorkspaceCreate,
			Errors: agentErrors("", ErrResp{Status: 409, When: "a workspace directory of that name already exists"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/update", Summary: "Rewrite a workspace manifest's editable fields (name, agent, autonomy, mcps, and skills — which names skill packages, not individual skills) in place. Comments, key order, and keys the editor does not own survive the write; an empty mcps or skills removes the key.",
			Request: agentWorkspaceEditRequest{}, Response: agentWorkspaceView{}, Handler: ctrl.AgentWorkspaceUpdate,
			Errors: agentErrors("no such workspace"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/delete", Summary: "End every live terminal a workspace's sessions hold and delete their records. The workspace directory itself is never touched — it is the user's, and possibly under version control.",
			Request: agentWorkspaceDirRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentWorkspaceDelete,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/open-in-editor", Summary: "Launch the configured editor (Settings › General) on the workspace directory, detached. Fails when no editor is configured or the command does not resolve on PATH.",
			Request: agentWorkspaceDirRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentWorkspaceOpenInEditor,
			Errors: agentErrors("no such workspace", ErrResp{Status: 400, When: "no editor is configured, or its command is not on PATH"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/reveal", Summary: "Open the workspace directory in the OS file manager.",
			Request: agentWorkspaceDirRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentWorkspaceReveal,
			Errors: agentErrors("no such workspace"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "mcps", Summary: "List the merged MCP catalogue: shipped entries plus the user's mcps.yaml, sorted by id, each with its stability, resolved command line, and any problem (a command that does not resolve on PATH). A user id shadowing a shipped one wins and says so.",
			Response: agentMCPCatalogueResponse{}, Handler: ctrl.AgentMCPCatalogue,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "mcps/import", Summary: "Parse pasted MCP JSON — claude's {\"mcpServers\": {...}} wrapper or a bare id-to-server map — into mcps.yaml and return the refreshed catalogue. An id already declared in mcps.yaml is a conflict; edit the file to change an existing entry.",
			Request: agentMCPImportRequest{}, Response: agentMCPImportResponse{}, Handler: ctrl.AgentMCPImport,
			Errors: agentErrors("", ErrResp{Status: 409, When: "a pasted id is already declared in mcps.yaml"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "mcps/remove", Summary: "Delete a user-declared server from mcps.yaml and return the refreshed catalogue. Shipped entries are refused — a workspace disables one by dropping the id from its own mcps list.",
			Request: agentMCPRemoveRequest{}, Response: agentMCPCatalogueResponse{}, Handler: ctrl.AgentMCPRemove,
			Errors: agentErrors("no user-declared server of that id", ErrResp{Status: 400, When: "the id names a shipped entry"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "skills", Summary: "List the skill packages defined in skills.yml, each with the skills its glob patterns currently select. Names come from the skills this build ships (hive-*) and the ones authored under .shared/skills. A workspace carries a package's skills only by naming the package in its own skills list.",
			Response: agentSkillPackagesResponse{}, Handler: ctrl.AgentSkillPackages,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "skills/reveal", Summary: "Open skills.yml, where packages are defined, seeding it first if it is missing.",
			Status: http.StatusNoContent, Handler: ctrl.AgentSkillPackagesReveal,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "skills/shared", Summary: "Open the shared skills directory (.shared/skills) in the OS file manager, creating it if missing. A skill there is a SKILL.md in a directory named for it, and a package selects it by name.",
			Status: http.StatusNoContent, Handler: ctrl.AgentSharedSkillsReveal,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions", Summary: "List a workspace's sessions without regenerating its artifacts, unlike workspaces/open. terminalId is empty for a session with no live terminal.",
			Request: agentSessionsRequest{}, Response: agentSessionsResponse{}, Handler: ctrl.AgentSessions,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/all", Summary: "List every session across every workspace, newest first in stable creation order — the sidebar's cross-workspace read, unlike sessions which scopes to one workspace.",
			Response: agentSessionsAllResponse{}, Handler: ctrl.AgentSessionsAll,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/start", Summary: "Launch a new, named session in a workspace: resolves the workspace's agent, autonomy posture and MCP wiring into a command line, creates a detached tmux session named agentws-<id> running it, and attaches. cols/rows of 0x0 attach unsized. The data plane is the tmux stream at " + TerminalStreamPath + ", outside this operations table; windowId names the pane to frame input/output for.",
			Request: agentSessionStartRequest{}, Response: agentSessionView{}, Handler: ctrl.AgentSessionStart,
			Errors: agentErrors("no such workspace", ErrResp{Status: 503, When: "tmux is unavailable: an unsupported platform, missing tmux, or a server build"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/resume", Summary: "Reattach a session's live tmux session if it still has one, or relaunch it — resuming the agent's own conversation when it has a resume form (resumeAttempted), and starting a fresh one with a notice when it does not.",
			Request: agentSessionResumeRequest{}, Response: agentSessionView{}, Handler: ctrl.AgentSessionResume,
			Errors: agentErrors("no such session, or its workspace is gone", ErrResp{Status: 503, When: "tmux is unavailable: an unsupported platform, missing tmux, or a server build"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/close", Summary: "End a session's live tmux session and report whether there was one to close. The session record is untouched, so it still lists afterward.",
			Request: agentSessionIDRequest{}, Response: agentSessionCloseResponse{}, Handler: ctrl.AgentSessionClose,
			Errors: agentErrors("no such session"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/rename", Summary: "Set a session's display name. The record is the only thing touched — a live tmux session keeps its agentws-<id> name.",
			Request: agentSessionRenameRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentSessionRename,
			Errors: agentErrors("no such session"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/activity", Summary: "Classify live sessions from their captured tmux panes: ready, active, or approval — approval is the highest-urgency state. workspace scopes to one workspace; empty spans every workspace. A session with no live tmux session is omitted.",
			Request: agentSessionActivityRequest{}, Response: agentSessionActivityResponse{}, Handler: ctrl.AgentSessionActivity,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/resize", Summary: "Vote a size for a session's attached control client, the same renegotiation the terminal pane casts on a host resize; tmux answers on the stream with a window resized event, which is what sets the grid.",
			Request: agentSessionResizeRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentSessionResize,
			Errors: agentErrors("no such session, or it has no attached terminal"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/delete", Summary: "End any live terminal and delete a session's record.",
			Request: agentSessionIDRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentSessionDelete,
			Errors: agentErrors("no such session"),
		},
	}
}

// agentErrors documents what every agent-workspace operation can answer
// beyond the generic error: the bearer token these routes require, same as
// every other terminal-prefixed route.
func agentErrors(notFound string, extra ...ErrResp) []ErrResp {
	errs := []ErrResp{{Status: 401, When: "the Authorization: Bearer token is missing or wrong"}}
	if notFound != "" {
		errs = append(errs, ErrResp{Status: 404, When: notFound})
	}
	return append(errs, extra...)
}

// baseOperations is what is left of this adapter's own surface after the
// agent-facing API moved to MCP (ADR mcp-replaces-the-agent-facing-http-api): a liveness probe and the running
// build. Both stay HTTP because they answer the question "is the app up, and
// which build is it?" — one a shell script or a health check asks with a plain
// GET, and a JSON-RPC handshake is the wrong shape for it. Everything an agent
// drives is a tool on the MCP server now.
func (ctrl *Controller) baseOperations() []Op {
	return []Op{
		{
			Method: "GET", Path: "/api/version", Summary: "Report the running build's VCS identity.",
			Response: struct {
				Service string    `json:"service"`
				Build   web.Build `json:"build"`
			}{}, Handler: plain(ctrl.version),
		},
		{
			Method: "GET", Path: "/api/status", Summary: "Report whether the webhook listener is running and on which host and port.",
			Response: statusResponse{}, Handler: ctrl.Status,
		},
	}
}

func (ctrl *Controller) terminalOperations() []Op {
	return []Op{
		{
			Method: "POST", Path: "/api/terminal/attach", Summary: "Attach a tmux control-mode client to a session slug and return its windows. Attaching never spawns: a slug tmux is not running answers 404, and POST /api/terminal/start is what creates it. cols/rows are the opening size vote; 0x0 attaches without setting a client size, leaving the session at the size its other clients gave it. The data plane is a WebSocket served at " + TerminalStreamPath + ", outside this operations table.",
			Request: terminalAttachRequest{}, Response: terminalAttachResponse{}, Handler: ctrl.TerminalAttach,
			Errors: terminalErrors("the slug names no reachable tmux session"),
		},
		{
			Method: "POST", Path: "/api/terminal/start", Summary: "Spawn the tmux session a slug names, from the hive session's own spawn configuration — the windows, working directory and agent command hive itself would use — and report whether this call is what created it. A session tmux is already running answers started=false rather than being respawned. Starting runs the session's agent command, which is why it is a separate call from attach. The slug \"Scratch\" is reserved for the scratch terminal, which belongs to no hive session: starting it opens one window in the user's home directory, and so does every window added to it afterwards.",
			Request: terminalSlugRequest{}, Response: terminalStartResponse{}, Handler: ctrl.TerminalStart,
			Errors: terminalErrors("no hive session carries that slug",
				ErrResp{Status: 409, When: "the hive session is not active, so it has no checkout to open a terminal in"}),
		},
		{
			Method: "POST", Path: "/api/terminal/kill", Summary: "Kill the tmux session a slug names and report whether there was one to kill. This is the terminal's lifecycle only — the hive session, its checkout and its record are untouched — but whatever is running inside it, the agent included, stops. A slug tmux is not running answers killed=false rather than failing.",
			Request: terminalSlugRequest{}, Response: terminalKillResponse{}, Handler: ctrl.TerminalKill,
			Errors: terminalErrors(""),
		},
		{
			Method: "POST", Path: "/api/terminal/resize", Summary: "Vote a size for the attached control client; every client attached to a window renders the same grid and tmux's window-size option decides whose size that is.",
			Request: terminalSizeRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalResize,
			Errors: terminalErrors("no terminal is attached for that slug"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/new", Summary: "Create a window in a session and return its tmux window id. Attaching first is not required: a session with no control client gets its window from a one-shot, opened in the session's own working directory the same way an attached client's would be. A slug tmux is not running answers 404 — POST /api/terminal/start is what creates the session.",
			Request: terminalSlugRequest{}, Response: terminalNewWindowResponse{}, Handler: ctrl.TerminalNewWindow,
			Errors: terminalErrors("the slug names no running tmux session"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/close", Summary: "Kill one window of the attached session.",
			Request: terminalWindowRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalCloseWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/rename", Summary: "Rename one window of the attached session.",
			Request: terminalRenameRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalRenameWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/move", Summary: "Move one window of the attached session to a position in its window order and answer with the order tmux settled on. position is a 0-based index into the resulting order, the way a drop on a tab strip means one; tmux's own indices are renumbered afterwards so they stay contiguous. The active window is preserved, and the move is tmux session state — every other client attached to the session sees it too.",
			Request: terminalMoveRequest{}, Response: terminalWindowsResponse{}, Handler: ctrl.TerminalMoveWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window",
				ErrResp{Status: 400, When: "the position is outside the session's window order"}),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/select", Summary: "Make one window the attached session's active window.",
			Request: terminalWindowRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalSelectWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/list", Summary: "List several sessions' windows without attaching, keyed by slug. The whole set is answered from one tmux call. An attached slug answers from its live client; a slug with no tmux session behind it is absent from the answer rather than an error.",
			Request: terminalSlugsRequest{}, Response: terminalSessionWindowsResponse{}, Handler: ctrl.TerminalListWindows,
			Errors: terminalErrors(""),
		},
		{
			Method: "POST", Path: "/api/terminal/detach", Summary: "Close the control client, leaving the tmux session itself running.",
			Request: terminalSlugRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalDetach,
			Errors: terminalErrors("no terminal is attached for that slug"),
		},
	}
}

// terminalErrors documents what every terminal operation can answer beyond the
// generic error: the bearer token these — and only these — routes require, and
// tmux being absent or too old. An empty notFound means the operation has no
// 404: an absent target is one of its answers, not one of its failures. extra
// carries whatever else one operation alone can answer.
func terminalErrors(notFound string, extra ...ErrResp) []ErrResp {
	errs := []ErrResp{{Status: 401, When: "the Authorization: Bearer token is missing or wrong"}}
	if notFound != "" {
		errs = append(errs, ErrResp{Status: 404, When: notFound})
	}
	errs = append(errs, extra...)
	return append(errs, ErrResp{Status: 503, When: "tmux is unavailable: missing, older than 3.2, or an unsupported build"})
}

func (ctrl *Controller) Handler() http.Handler {
	chain := errchain.New(mid.Errors(ctrl.log, mapAppError))

	mux := http.NewServeMux()
	preflighted := map[string]bool{}
	for _, op := range ctrl.operations() {
		handler := chain.ToHandlerFunc(op.Handler)
		if strings.HasPrefix(op.Path, TerminalPathPrefix) {
			handler = ctrl.cors.wrap(handler)
			if !preflighted[op.Path] {
				preflighted[op.Path] = true
				mux.HandleFunc("OPTIONS "+op.Path, ctrl.cors.preflight)
			}
		}
		mux.HandleFunc(op.pattern(), handler)
	}
	return mid.Logger(ctrl.log, "/api/status", "/api/version")(mux)
}

// plain adapts a pre-built handler (the shared version handler) to the error
// chain; it writes its own response and never fails.
func plain(h http.HandlerFunc) errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		h(w, r)
		return nil
	}
}

// mapAppError maps a core error's Kind to an HTTP status exactly once; an
// *app.Error marshals to {kind, message}.
func mapAppError(err error) (int, any, bool) {
	appErr, ok := errors.AsType[*app.Error](err)
	if !ok {
		return 0, nil, false
	}
	return statusForKind(appErr.Kind), appErr, true
}

func statusForKind(k app.Kind) int {
	switch k {
	case app.KindInvalid:
		return http.StatusBadRequest
	case app.KindNotFound:
		return http.StatusNotFound
	case app.KindConflict:
		return http.StatusConflict
	case app.KindUnauthenticated:
		return http.StatusUnauthorized
	case app.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
