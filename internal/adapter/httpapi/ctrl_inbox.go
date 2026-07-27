package httpapi

import (
	"cmp"
	"context"
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/web/extractors"
)

type InboxQuery struct {
	Profile    string `schema:"profile"`
	ExternalID string `schema:"externalId"`
	Feed       string `schema:"feed"`
	Archived   bool   `schema:"archived"`
	Limit      int    `schema:"limit"`
}

func (q InboxQuery) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("profile", q.Profile,
			criterio.When(q.Feed != "", requiredWith[string]("feed"))),
		criterio.Run("limit", q.Limit,
			criterio.SkipIf(q.Limit == 0, criterio.Positive[int]())),
	)
}

type itemsResponse struct {
	Items []store.InboxItemView `json:"items"`
}

func (ctrl *Controller) InboxList(w http.ResponseWriter, r *http.Request) error {
	q, err := extractors.Query[InboxQuery](r)
	if err != nil {
		return err
	}

	ctx := r.Context()
	limit := cmp.Or(q.Limit, defaultListLimit)

	var items []store.InboxItemView
	switch {
	case q.ExternalID != "":
		items, err = ctrl.core.Inbox.FindItems(ctx, q.Profile, q.ExternalID)
	case q.Feed != "" && q.Archived:
		items, err = ctrl.core.Inbox.ListArchivedInboxItemsByFeed(ctx, q.Profile, q.Feed, limit)
	case q.Feed != "":
		items, err = ctrl.core.Inbox.ListInboxItemsByFeed(ctx, q.Profile, q.Feed, limit)
	default:
		items, err = ctrl.core.Inbox.ListItems(ctx, q.Profile, limit)
	}
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, itemsResponse{Items: items})
}

type EventsQuery struct {
	ItemID     int64  `schema:"itemId"`
	ExternalID string `schema:"externalId"`
	Profile    string `schema:"profile"`
	Limit      int    `schema:"limit"`
}

func (q EventsQuery) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("itemId", q.ItemID,
			criterio.When(q.ExternalID == "", requiredWithout[int64]("externalId"))),
		criterio.Run("limit", q.Limit,
			criterio.SkipIf(q.Limit == 0, criterio.Positive[int]())),
	)
}

type eventsResponse struct {
	Events []store.InboxEventView `json:"events"`
}

func (ctrl *Controller) InboxItemEvents(w http.ResponseWriter, r *http.Request) error {
	q, err := extractors.Query[EventsQuery](r)
	if err != nil {
		return err
	}
	itemID, err := ctrl.resolveItemID(r.Context(), q)
	if err != nil {
		return err
	}
	events, err := ctrl.core.Inbox.InboxItemEvents(r.Context(), itemID, cmp.Or(q.Limit, defaultEventLimit))
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, eventsResponse{Events: events})
}

// resolveItemID accepts an explicit itemId, or an externalId that must resolve
// to exactly one item.
func (ctrl *Controller) resolveItemID(ctx context.Context, q EventsQuery) (int64, error) {
	if q.ItemID != 0 {
		return q.ItemID, nil
	}
	items, err := ctrl.core.Inbox.FindItems(ctx, q.Profile, q.ExternalID)
	if err != nil {
		return 0, err
	}
	switch len(items) {
	case 0:
		return 0, app.Errorf(app.KindNotFound, "no item with external id %q", q.ExternalID)
	case 1:
		return items[0].ID, nil
	default:
		return 0, app.Errorf(app.KindConflict, "external id %q matches %d items; add profile to disambiguate", q.ExternalID, len(items))
	}
}
