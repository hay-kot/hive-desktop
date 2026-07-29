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

	// terminalToken and cors apply to the terminal operations alone. Both are
	// composed in main.go — the core carries no transport credential (ADR 0032).
	terminalToken string
	cors          corsPolicy
}

// New builds the controller. terminalToken and origins govern the terminal
// control plane only: every other operation here is deliberately
// unauthenticated behind the loopback bind (ADR 0021). An empty terminalToken
// means terminal mode is off for this run — its operations are not registered
// at all (ADR 0033).
func New(core *app.App, log zerolog.Logger, terminalToken string, origins []string) *Controller {
	return &Controller{
		core:          core,
		log:           log,
		version:       web.VersionHandler("hive.desktop.api"),
		terminalToken: terminalToken,
		cors:          corsPolicy{origins: origins, log: log},
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
