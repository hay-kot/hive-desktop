// Package httpapi is the agent-facing HTTP adapter over app.App: a loopback
// control surface an agent drives to observe and operate the app without
// reading SQLite or the config files directly. It began as read + reload
// (ADR 0021) and is growing toward full agentic control — reads, reloads, and
// mutations like setting a profile's avatar — the same core methods a future
// MCP adapter will expose as tools. It mounts onto the webhook listener's
// loopback server, which also hosts the optional pprof handler (ADR 0023).
//
// The surface is self-describing (ADR 0027): one operations table
// (Controller.operations) is the single source the mux, the GET /api route
// index, and the GET /api/openapi.json document are all built from, so an agent
// discovers the API in one call rather than reverse-engineering the binary.
package httpapi

import (
	"fmt"
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web"
)

// PathPrefix is where App mounts this handler on the webhook listener.
const PathPrefix = "/api/"

const (
	defaultListLimit  = 200
	defaultEventLimit = 50
)

type Controller struct {
	core    *app.App
	log     zerolog.Logger
	version http.HandlerFunc

	// terminalToken and cors apply to the token-guarded operations (terminal,
	// pop-up terminal, and agent workspaces) alone. Both are composed in
	// main.go — the core carries no transport credential (ADR 0036).
	terminalToken string
	cors          corsPolicy
	opts          Options
}

// Options configures the token-guarded surfaces: the bearer token and CORS
// allowlist every one of them shares, and which route groups are mounted at
// all. TerminalEnabled and AgentsEnabled are independent axes — either alone
// is enough for TerminalToken to be minted in main.go, because the agent
// control plane and the PTY stream a workspace session rides sit under the
// same token-guarded prefix as the tmux/pop-up terminal surface (ADR 0061).
type Options struct {
	TerminalToken   string
	Origins         []string
	TerminalEnabled bool
	AgentsEnabled   bool
}

// New builds the controller. Every operation outside opts' route groups is
// deliberately unauthenticated behind the loopback bind (ADR 0021).
// TerminalEnabled/AgentsEnabled false means that group's operations are not
// registered at all — off is absence, not a 503 (ADR 0037).
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

func requiredWith[T comparable](other string) criterio.Validator[T] {
	return func(val T) error {
		var zero T
		if val == zero {
			return fmt.Errorf("is required when %s is set", other)
		}
		return nil
	}
}

func requiredWithout[T comparable](other string) criterio.Validator[T] {
	return func(val T) error {
		var zero T
		if val == zero {
			return fmt.Errorf("is required when %s is not set", other)
		}
		return nil
	}
}
