// Package httpapi is the HTTP adapter over app.App, mounted on the webhook
// listener's loopback server, which also hosts the optional pprof handler
// (ADR pprof-debug-endpoint).
//
// It serves two unrelated things, and the split is the point:
//
// The terminal, pop-up terminal and agent-workspace control planes under
// /api/terminal/ are the Wails frontend's own transport, not an agent surface.
// They authenticate with a per-run bearer token because each of them spawns a
// process (ADR terminal-transport, ADR a-workspace-declares-its-own-authority), and their data planes are raw WebSocket mounts
// outside the operations table.
//
// /api/status and /api/version are the liveness probe — the plain-GET way to
// ask whether the app is up and which build it is, which a shell script or a
// health check needs and JSON-RPC is the wrong shape for.
//
// What is *not* here any more is the agent-facing control surface this package
// began as (ADR agent-http-api, ADR self-describing-agent-api). Inbox, feeds, profiles, actions, source
// refresh and flow dry runs are tools on the MCP server now
// (internal/adapter/mcpsrv, ADR mcp-replaces-the-agent-facing-http-api), and so is the self-description the
// OpenAPI document used to provide.
package httpapi

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web"
)

// PathPrefix is where App mounts this handler on the webhook listener.
const PathPrefix = "/api/"

type Controller struct {
	core    *app.App
	log     zerolog.Logger
	version http.HandlerFunc

	// terminalToken and cors apply to the token-guarded operations (terminal,
	// pop-up terminal, and agent workspaces) alone. Both are composed in
	// main.go — the core carries no transport credential (ADR terminal-transport).
	terminalToken string
	cors          corsPolicy
	opts          Options
}

// Options configures the token-guarded surfaces: the bearer token and CORS
// allowlist every one of them shares, and which route groups are mounted at
// all. TerminalEnabled and AgentsEnabled are independent axes — either alone
// is enough for TerminalToken to be minted in main.go, because the agent
// control plane and the PTY stream a workspace session rides sit under the
// same token-guarded prefix as the tmux/pop-up terminal surface (ADR a-workspace-declares-its-own-authority).
type Options struct {
	TerminalToken   string
	Origins         []string
	TerminalEnabled bool
	AgentsEnabled   bool
}

// New builds the controller. The liveness routes are deliberately
// unauthenticated behind the loopback bind (ADR agent-http-api).
// TerminalEnabled/AgentsEnabled false means that group's operations are not
// registered at all — off is absence, not a 503 (ADR terminal-experimental-gate).
func New(core *app.App, log zerolog.Logger, opts Options) *Controller {
	return &Controller{
		core:          core,
		log:           log,
		version:       web.VersionHandler("hive.desktop.api"),
		terminalToken: opts.TerminalToken,
		cors:          corsPolicy{origins: opts.Origins, log: log},
		opts:          opts,
	}
}
