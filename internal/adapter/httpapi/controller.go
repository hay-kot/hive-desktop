// Package httpapi is the agent-facing HTTP adapter over app.App: a loopback
// control surface an agent drives to observe and operate the app without
// reading SQLite or the config files directly. It began as read + reload
// (ADR 0021) and is growing toward full agentic control — reads, reloads, and
// mutations like setting a profile's avatar — the same core methods a future
// MCP adapter will expose as tools. It mounts onto the webhook listener's
// loopback server, which also hosts the optional pprof handler (ADR 0023).
package httpapi

import (
	"fmt"

	"github.com/hay-kot/criterio"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// PathPrefix is where App mounts this handler on the webhook listener.
const PathPrefix = "/api/"

const (
	defaultListLimit  = 200
	defaultEventLimit = 50
)

type Controller struct {
	core *app.App
	log  zerolog.Logger
}

func New(core *app.App, log zerolog.Logger) *Controller {
	return &Controller{core: core, log: log}
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
