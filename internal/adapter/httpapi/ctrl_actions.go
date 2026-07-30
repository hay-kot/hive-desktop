package httpapi

import (
	"net/http"

	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
)

// actionsResponse is the actions catalog as the running app holds it. An
// actions.yml that fails to parse leaves the previous catalog in effect, so
// the action list alone cannot tell an editor whether its write was accepted:
// valid and error are what close that loop, the same way a profile reports
// valid (issue #111). Path names the file to fix when it did not.
type actionsResponse struct {
	Path    string                   `json:"path"`
	Valid   bool                     `json:"valid"`
	Error   string                   `json:"error"`
	Actions []actions.EditableAction `json:"actions"`
}

// Actions lists the effective action catalog with its load status.
func (ctrl *Controller) Actions(w http.ResponseWriter, r *http.Request) error {
	catalog := ctrl.core.Actions.List(r.Context())
	return server.JSON(w, http.StatusOK, actionsResponse{
		Path:    ctrl.core.RuntimePaths().ActionsPath,
		Valid:   catalog.Error == "",
		Error:   catalog.Error,
		Actions: catalog.Actions,
	})
}
