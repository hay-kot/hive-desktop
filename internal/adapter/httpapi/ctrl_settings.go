package httpapi

import (
	"net/http"

	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
)

type restartPendingField struct {
	Field     string `json:"field"     jsonschema:"description=Dotted settings path, or bootstrap.data_dir / bootstrap.config_dir for a directory override."`
	Reason    string `json:"reason"    jsonschema:"description=Why the running process cannot adopt this value."`
	Running   string `json:"running"   jsonschema:"description=The value this process started with."`
	Persisted string `json:"persisted" jsonschema:"description=The value on disk, which a relaunch would pick up."`
}

type settingsReloadResponse struct {
	Changed        []string              `json:"changed"        jsonschema:"description=Dotted paths of the fields that differ from the settings this process was serving."`
	RestartPending []restartPendingField `json:"restartPending" jsonschema:"description=Persisted values this process is not running and cannot adopt without a relaunch."`
}

type settingsStatusResponse struct {
	Path           string                `json:"path"           jsonschema:"description=The settings.yaml this process reads."`
	Valid          bool                  `json:"valid"          jsonschema:"description=Whether the file on disk currently parses and validates; false means the app is serving its last good values."`
	Error          string                `json:"error"          jsonschema:"description=Why the last reload failed; empty when valid."`
	RestartPending []restartPendingField `json:"restartPending" jsonschema:"description=Persisted values this process is not running and cannot adopt without a relaunch."`
}

func (ctrl *Controller) SettingsReload(w http.ResponseWriter, r *http.Request) error {
	result, err := ctrl.core.ReloadSettings(r.Context())
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, settingsReloadView(result))
}

func (ctrl *Controller) SettingsStatus(w http.ResponseWriter, r *http.Request) error {
	return server.JSON(w, http.StatusOK, settingsStatusView(ctrl.core.SettingsStatus(r.Context())))
}

func settingsReloadView(result app.SettingsReload) settingsReloadResponse {
	view := settingsReloadResponse{
		Changed:        result.Changed,
		RestartPending: restartPendingView(result.RestartPending),
	}
	if view.Changed == nil {
		view.Changed = []string{}
	}
	return view
}

func settingsStatusView(status app.SettingsStatus) settingsStatusResponse {
	return settingsStatusResponse{
		Path:           status.Path,
		Valid:          status.Valid,
		Error:          status.Error,
		RestartPending: restartPendingView(status.RestartPending),
	}
}

func restartPendingView(fields []app.RestartPendingField) []restartPendingField {
	view := make([]restartPendingField, 0, len(fields))
	for _, field := range fields {
		view = append(view, restartPendingField(field))
	}
	return view
}
