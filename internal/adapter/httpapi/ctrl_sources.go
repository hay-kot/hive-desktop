package httpapi

import (
	"net/http"

	"github.com/hay-kot/httpkit/server"
)

type refreshResponse struct {
	Sources  int `json:"sources"  jsonschema:"description=Number of sources that ran this tick."`
	Appended int `json:"appended" jsonschema:"description=Number of inbox items appended across all sources."`
	Failed   int `json:"failed"   jsonschema:"description=Number of sources that failed this tick."`
}

func (ctrl *Controller) SourcesRefresh(w http.ResponseWriter, r *http.Request) error {
	sum, err := ctrl.core.RefreshSources(r.Context())
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, refreshResponse{
		Sources: sum.Sources, Appended: sum.Appended, Failed: sum.Failed,
	})
}
