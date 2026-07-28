package httpapi

import (
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/web/extractors"
)

type FeedsQuery struct {
	Profile string `schema:"profile" desc:"Profile id to list feeds for; ids come from GET /api/profiles." example:"hive" required:"true"`
}

func (q FeedsQuery) Validate() error {
	return criterio.Run("profile", q.Profile, criterio.Required)
}

type feedsResponse struct {
	Feeds []store.FeedInboxCount `json:"feeds"`
}

func (ctrl *Controller) Feeds(w http.ResponseWriter, r *http.Request) error {
	q, err := extractors.Query[FeedsQuery](r)
	if err != nil {
		return err
	}
	counts, err := ctrl.core.Inbox.FeedCounts(r.Context(), q.Profile)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, feedsResponse{Feeds: counts})
}
