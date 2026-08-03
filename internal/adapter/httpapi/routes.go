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

// RawBinary marks a request or response body as raw bytes of one of the given
// media types rather than JSON. Note is a one-line human description surfaced
// in both the route index and the OpenAPI requestBody.
type RawBinary struct {
	Media []string
	Note  string
}

// Op is one HTTP operation. The operations table is the single source the mux,
// the GET /api index, and the OpenAPI document are all built from, so none can
// drift. Request and Response are each a zero-value struct (a JSON body,
// reflected into a schema), a RawBinary, or nil (no body).
type Op struct {
	Method   string
	Path     string
	Summary  string
	Query    any // struct whose `schema`-tagged fields become query parameters
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

func (op Op) successStatus() int {
	if op.Status == 0 {
		return http.StatusOK
	}
	return op.Status
}

// pattern is the ServeMux pattern this op registers under. A path ending in "/"
// is anchored with {$} so it matches only that exact path: the index lives at
// the mount root /api/ (a request for /api is redirected there by the listener
// that mounts this handler), and without the anchor it would become a subtree
// that swallows every otherwise-unmatched /api/… request instead of 404ing.
func (op Op) pattern() string {
	path := op.Path
	if strings.HasSuffix(path, "/") {
		path += "{$}"
	}
	return op.Method + " " + path
}

func (ctrl *Controller) operations() []Op {
	ops := ctrl.baseOperations()
	// Off means the surface is absent rather than answering 503, so the route
	// index and OpenAPI document never advertise something that cannot work
	// (ADR 0037 point 2). The two flags gate independently: a build can ship
	// agents without terminal mode or vice versa.
	if ctrl.opts.TerminalEnabled {
		ops = append(ops, ctrl.terminalOperations()...)
		ops = append(ops, ctrl.popupTerminalOperations()...)
	}
	if ctrl.opts.AgentsEnabled {
		ops = append(ops, ctrl.agentOperations()...)
	}
	return ops
}

// popupTerminalOperations is the ephemeral surface: terminals this process owns
// outright, opened on demand and addressed by an id it mints (ADR 0048). They
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
			Method: "POST", Path: PopupTerminalPathPrefix + "launchers", Summary: "List the configured launchers — the launchers list in actions.yml — in file order. What each one runs is deliberately absent: open it by id.",
			Response: popupLauncherListResponse{}, Handler: ctrl.PopupTerminalLaunchers,
			Errors: popupTerminalErrors(""),
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
// execution (ADR 0036, ADR 0061).
func (ctrl *Controller) agentOperations() []Op {
	return []Op{
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces", Summary: "List every recognized agent workspace under the configured root, valid or not. A workspace whose manifest fails to parse still lists with its last-good name and agent, plus a problem explaining what is wrong. available/error report whether ephemeral terminals can run at all in this build; root is the configured workspace root regardless of that answer.",
			Response: agentWorkspacesResponse{}, Handler: ctrl.AgentWorkspaces,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/open", Summary: "Regenerate a workspace's disposable artifacts (CLAUDE.md, .mcp.json, .codex/config.toml, .claude/, .agents/, an empty docs/) from its manifest and return its sessions. This is the only call that writes into a workspace; missingMcps names declared MCP ids the catalogue does not resolve.",
			Request: agentWorkspaceOpenRequest{}, Response: agentWorkspaceOpenResponse{}, Handler: ctrl.AgentWorkspaceOpen,
			Errors: agentErrors("no such workspace, or its manifest is invalid"),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "workspaces/delete", Summary: "End every live terminal a workspace's sessions hold and delete their records. The workspace directory itself is never touched — it is the user's, and possibly under version control.",
			Request: agentWorkspaceDeleteRequest{}, Status: http.StatusNoContent, Handler: ctrl.AgentWorkspaceDelete,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions", Summary: "List a workspace's sessions without regenerating its artifacts, unlike workspaces/open. terminalId is empty for a session with no live terminal.",
			Request: agentSessionsRequest{}, Response: agentSessionsResponse{}, Handler: ctrl.AgentSessions,
			Errors: agentErrors(""),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/start", Summary: "Launch a new, named session in a workspace: resolves the workspace's agent, autonomy posture and MCP wiring into a command line and opens it on a Hive-owned PTY. cols/rows of 0x0 open at the terminal's own default. The data plane is the ptyterm stream at " + PTYStreamPath + ", outside this operations table.",
			Request: agentSessionStartRequest{}, Response: agentSessionView{}, Handler: ctrl.AgentSessionStart,
			Errors: agentErrors("no such workspace", ErrResp{Status: 503, When: "ephemeral terminals are unavailable: an unsupported platform or a server build"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/resume", Summary: "Reattach a session's live terminal if it still has one, or relaunch it — resuming the agent's own conversation when it has a resume form (resumeAttempted), and starting a fresh one with a notice when it does not.",
			Request: agentSessionResumeRequest{}, Response: agentSessionView{}, Handler: ctrl.AgentSessionResume,
			Errors: agentErrors("no such session, or its workspace is gone", ErrResp{Status: 503, When: "ephemeral terminals are unavailable: an unsupported platform or a server build"}),
		},
		{
			Method: "POST", Path: AgentWorkspacesPathPrefix + "sessions/close", Summary: "End a session's live terminal and report whether there was one to close. The session record is untouched, so it still lists afterward.",
			Request: agentSessionIDRequest{}, Response: agentSessionCloseResponse{}, Handler: ctrl.AgentSessionClose,
			Errors: agentErrors("no such session"),
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

func (ctrl *Controller) baseOperations() []Op {
	return []Op{
		{
			Method: "GET", Path: "/api/", Summary: "List every route this API serves, with a link to the OpenAPI document.",
			Response: apiIndex{}, Handler: ctrl.APIIndex,
		},
		{
			Method: "GET", Path: openAPIPath, Summary: "The OpenAPI description of this API; servers are set to the address it was fetched from.",
			Handler: ctrl.OpenAPI,
		},
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
		{
			Method: "GET", Path: "/api/feeds", Summary: "List a profile's feeds with unread and archived counts.",
			Query: FeedsQuery{}, Response: feedsResponse{}, Handler: ctrl.Feeds,
			Errors: []ErrResp{{Status: 422, When: "the query failed validation (profile is required)"}},
		},
		{
			Method: "GET", Path: "/api/inbox", Summary: "List a profile's inbox items, optionally filtered by feed or external id; each item carries a feedId (the claiming feed, empty when unrouted) and a payload of the source's raw JSON.",
			Query: InboxQuery{}, Response: itemsResponse{}, Handler: ctrl.InboxList,
			Errors: []ErrResp{{Status: 422, When: "the query failed validation (profile is required when feed is set)"}},
		},
		{
			Method: "GET", Path: "/api/inbox/events", Summary: "List one inbox item's lifecycle events, resolved by itemId or a unique externalId; each event's detail is source-specific raw JSON.",
			Query: EventsQuery{}, Response: eventsResponse{}, Handler: ctrl.InboxItemEvents,
			Errors: []ErrResp{
				{Status: 422, When: "neither itemId nor externalId was given"},
				{Status: 404, When: "no item matches the externalId"},
				{Status: 409, When: "the externalId matches items in more than one profile; add profile to disambiguate"},
			},
		},
		{
			Method: "POST", Path: "/api/sources/refresh", Summary: "Force one producer tick across all sources, dropping fetch caches; returns aggregate totals, not a per-source breakdown.",
			Response: refreshResponse{}, Handler: ctrl.SourcesRefresh,
			Errors: []ErrResp{{Status: 503, When: "no producer is available (e.g. mock mode)"}},
		},
		{
			Method: "GET", Path: "/api/actions", Summary: "List the action catalog with the actions.yml it was loaded from and whether the file on disk currently parses; an invalid edit leaves the previous catalog in effect and reports its error here.",
			Response: actionsResponse{}, Handler: ctrl.Actions,
		},
		{
			Method: "GET", Path: "/api/profiles", Summary: "List every profile with its load status and whether it has an avatar.",
			Response: profilesResponse{}, Handler: ctrl.Profiles,
		},
		{
			Method: "POST", Path: "/api/profiles", Summary: "Create a profile, seeded with the starter graph when exactly one GitHub account is connected.",
			Request: createProfileRequest{}, Response: profileView{}, Status: http.StatusCreated, Handler: ctrl.CreateProfile,
			Errors: []ErrResp{{Status: 422, When: "name is missing or invalid"}},
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}", Summary: "Delete a profile, its flow files, its avatar, and its inbox state.",
			Status: http.StatusNoContent, Handler: ctrl.DeleteProfile,
		},
		{
			Method: "GET", Path: "/api/profiles/{id}/image", Summary: "Return a profile's avatar as a 128x128 PNG, or 404 when it has none.",
			Response: RawBinary{Media: []string{"image/png"}}, Handler: ctrl.GetProfileImage,
			Errors: []ErrResp{{Status: 404, When: "the profile has no image, or no such profile"}},
		},
		{
			Method: "PUT", Path: "/api/profiles/{id}/image", Summary: "Set a profile's avatar from the raw request body; the image is normalized to a 128x128 PNG.",
			Request: RawBinary{
				Media: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				Note:  "Send the image as the raw request body (PNG, JPEG, GIF, or WebP) — not multipart/form-data.",
			}, Response: profileView{}, Handler: ctrl.SetProfileImage,
			Errors: []ErrResp{
				{Status: 400, When: "the body was unreadable or not a supported image"},
				{Status: 404, When: "no such profile"},
			},
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}/image", Summary: "Clear a profile's avatar so its rail reverts to the letter chip.",
			Response: profileView{}, Handler: ctrl.ClearProfileImage,
		},
		{
			Method: "POST", Path: "/api/flows/execute", Summary: "Dry-run a flow against input you supply and report what every node did, committing nothing — no feed membership, inbox rows, notifications, queued actions or durable kv. Name the flow with exactly one of flowId (an installed flow, enabled or not), flow (a flow document as a JSON object, same schema as flows/<id>.yaml, version included) or flowYaml (that document as YAML text), so an unsaved edit can be executed before it is deployed. messages are delivered to nodeId — any node, not only a source, which is how one function node is exercised in isolation against a captured payload; a message carrying a Snapshot expands into its items and declares feed reconciliation exactly as a poll would, and an empty Snapshot is still a snapshot. Sources never fetch: a source node relays what you inject. Envelope fields you leave empty are filled in — ID gets a synthetic one, and Topic the source's own topic when injecting at a source node. kv seeds an in-memory sandbox (nodeId -> key -> value) that is the whole world a kv.get sees, so notify-once logic is testable against a known starting state; what the run would have written comes back in kvMutations. Each node reports what it received, what it emitted per output port (including ports with no wire behind them), its drops, timing, console output, and a structured error with line and column for a script failure.",
			Request: flowExecuteRequest{}, Response: flowExecuteResponse{}, Handler: ctrl.FlowExecute,
			Errors: []ErrResp{
				{Status: 400, When: "the flow document did not parse or validate, the flow cannot be built (a script that does not compile, a graph that is not a DAG), or nodeId names no node in it"},
				{Status: 404, When: "flowId names no installed flow"},
				{Status: 422, When: "the body failed validation (not exactly one flow source, or a missing nodeId or messages)"},
			},
		},
		{
			Method: "GET", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Return a webhook source node's feed-mark image as a 128x128 PNG, or 404 when it has none.",
			Response: RawBinary{Media: []string{"image/png"}}, Handler: ctrl.GetNodeImage,
			Errors: []ErrResp{{Status: 404, When: "the node has no image, or no such flow or node"}},
		},
		{
			Method: "PUT", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Set a webhook source node's feed-mark image from the raw request body; it is normalized to a 128x128 PNG and shown on the source's items instead of its icon.",
			Request: RawBinary{
				Media: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				Note:  "Send the image as the raw request body (PNG, JPEG, GIF, or WebP) — not multipart/form-data.",
			}, Response: nodeImageView{}, Handler: ctrl.SetNodeImage,
			Errors: []ErrResp{
				{Status: 400, When: "the body was unreadable or not a supported image, or the node is not a webhook source"},
				{Status: 404, When: "no such flow or node"},
			},
		},
		{
			Method: "DELETE", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Clear a webhook source node's feed-mark image so it reverts to its icon.",
			Response: nodeImageView{}, Handler: ctrl.ClearNodeImage,
			Errors: []ErrResp{{Status: 404, When: "no such flow or node"}},
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
			Method: "POST", Path: "/api/terminal/start", Summary: "Spawn the tmux session a slug names, from the hive session's own spawn configuration — the windows, working directory and agent command hive itself would use — and report whether this call is what created it. A session tmux is already running answers started=false rather than being respawned. Starting runs the session's agent command, which is why it is a separate call from attach.",
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
			Method: "POST", Path: "/api/terminal/windows/new", Summary: "Create a window in the attached session and return its tmux window id.",
			Request: terminalSlugRequest{}, Response: terminalNewWindowResponse{}, Handler: ctrl.TerminalNewWindow,
			Errors: terminalErrors("no terminal is attached for that slug"),
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
