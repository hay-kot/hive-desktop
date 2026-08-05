package mcpsrv

import (
	"errors"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// toolError maps a core error's Kind onto what an agent reads, exactly once
// for this adapter.
//
// The SDK packs an error returned from a tool handler into the tool result
// with IsError set, rather than failing the JSON-RPC call — which is the
// behaviour we want for every Kind: an agent that named a profile which does
// not exist should read "not_found" and correct itself, not lose its session
// to a protocol error.
//
// The Kind leads the message so it stays machine-readable without the agent
// parsing prose, the same contract the REST surface had when it put
// {kind, message} on the wire.
//
// Only Msg crosses, never the wrapped cause — the same split app.Error's own
// MarshalJSON makes, where the cause is for the log. That means a wrapped
// error can reach an agent as bare context ("creating action \"x\""), so the
// cause is logged here rather than dropped: the answer to "why did that tool
// say that?" has to exist somewhere.
func (ctrl *Controller) toolError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		if appErr.Err != nil {
			ctrl.log.Debug().Err(err).Str("kind", string(appErr.Kind)).Msg("mcp tool refused a call")
		}
		return fmt.Errorf("%s: %s", appErr.Kind, appErr.Msg)
	}
	// Unclassified means internal by definition — nobody decided otherwise.
	// Its text is for the log, not for the model.
	ctrl.log.Error().Err(err).Msg("mcp tool failed")
	return fmt.Errorf("%s: the app could not complete the request", app.KindInternal)
}
