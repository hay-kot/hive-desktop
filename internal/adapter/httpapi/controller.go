// Package httpapi is the agent-facing HTTP adapter over app.App: a loopback
// read + reload surface for observing the pipeline without reading SQLite. It
// mounts onto the webhook listener's loopback server (ADR 0019).
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
