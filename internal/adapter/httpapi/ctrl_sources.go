package httpapi

import (
	"net/http"

	"github.com/hay-kot/httpkit/server"
)

type refreshResponse struct {
	Sources  int `json:"sources"`
	Appended int `json:"appended"`
	Failed   int `json:"failed"`
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
