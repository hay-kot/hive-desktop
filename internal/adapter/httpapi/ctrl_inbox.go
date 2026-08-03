package httpapi

import (
	"cmp"
	"context"
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/web/extractors"
)

type InboxQuery struct {
	Profile    string `schema:"profile"    desc:"Profile id to scope to (from GET /api/profiles). Required when 'feed' is set."           example:"hive"`
	ExternalID string `schema:"externalId" desc:"A source's own id (e.g. a GitHub node id); returns every matching item across profiles."`
	Feed       string `schema:"feed"       desc:"Feed id from GET /api/feeds (e.g. 'hive/desktop-prs') to return only that feed's items." example:"hive/desktop-prs"`
	Archived   bool   `schema:"archived"   desc:"When true, list archived items instead of active ones."`
	Limit      int    `schema:"limit"      desc:"Maximum items to return; defaults to 200."`
}

func (q InboxQuery) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("profile", q.Profile,
			criterio.When(q.Feed != "", requiredWith[string]("feed"))),
		criterio.Run("limit", q.Limit,
			criterio.SkipIf(q.Limit == 0, criterio.Positive[int]())),
	)
}

// inboxItemView is the store view plus the feed that claims the item, so a flat
// listing can answer "which feed is this in?" — a store.InboxItemView carries no
// feed id of its own (the sidebar groups by feed a different way).
type inboxItemView struct {
	store.InboxItemView
	FeedID string `json:"feedId"`
}

type itemsResponse struct {
	Items []inboxItemView `json:"items"`
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

	views, err := ctrl.itemsWithFeed(ctx, items)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, itemsResponse{Items: views})
}

// itemsWithFeed annotates each item with the feed that claims it (empty when
// unrouted), resolved in one query so the listing avoids an N+1.
func (ctrl *Controller) itemsWithFeed(ctx context.Context, items []store.InboxItemView) ([]inboxItemView, error) {
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	feeds, err := ctrl.core.Inbox.InboxItemFeeds(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]inboxItemView, len(items))
	for i, it := range items {
		out[i] = inboxItemView{InboxItemView: it, FeedID: feeds[it.ID]}
	}
	return out, nil
}

type EventsQuery struct {
	ItemID     int64  `schema:"itemId"     desc:"Inbox item id. Provide this or externalId."`
	ExternalID string `schema:"externalId" desc:"External id resolving to one item; provide this or itemId. If it matches items in more than one profile, add 'profile' to disambiguate, else the response is 409."`
	Profile    string `schema:"profile"    desc:"Profile id used to disambiguate an externalId that matches multiple profiles."`
	Limit      int    `schema:"limit"      desc:"Maximum events to return; defaults to 50."`
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
	itemID, err := ctrl.resolveItemID(r.Context(), q.ItemID, q.ExternalID, q.Profile)
	if err != nil {
		return err
	}
	events, err := ctrl.core.Inbox.InboxItemEvents(r.Context(), itemID, cmp.Or(q.Limit, defaultEventLimit))
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, eventsResponse{Events: events})
}

type ItemSessionsQuery struct {
	ItemID     int64  `schema:"itemId"     desc:"Inbox item id. Provide this or externalId."`
	ExternalID string `schema:"externalId" desc:"External id resolving to one item; provide this or itemId. If it matches items in more than one profile, add 'profile' to disambiguate, else the response is 409."`
	Profile    string `schema:"profile"    desc:"Profile id used to disambiguate an externalId that matches multiple profiles."`
}

func (q ItemSessionsQuery) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("itemId", q.ItemID,
			criterio.When(q.ExternalID == "", requiredWithout[int64]("externalId"))),
	)
}

type itemSessionsResponse struct {
	Sessions []dispatch.ItemSessionView `json:"sessions"`
}

func (ctrl *Controller) InboxItemSessions(w http.ResponseWriter, r *http.Request) error {
	q, err := extractors.Query[ItemSessionsQuery](r)
	if err != nil {
		return err
	}
	itemID, err := ctrl.resolveItemID(r.Context(), q.ItemID, q.ExternalID, q.Profile)
	if err != nil {
		return err
	}
	sessions, err := ctrl.core.Sessions.ItemSessions(r.Context(), itemID)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, itemSessionsResponse{Sessions: sessions})
}

// resolveItemID accepts an explicit itemId, or an externalId that must resolve
// to exactly one item.
func (ctrl *Controller) resolveItemID(ctx context.Context, itemID int64, externalID, profile string) (int64, error) {
	if itemID != 0 {
		return itemID, nil
	}
	items, err := ctrl.core.Inbox.FindItems(ctx, profile, externalID)
	if err != nil {
		return 0, err
	}
	switch len(items) {
	case 0:
		return 0, app.Errorf(app.KindNotFound, "no item with external id %q", externalID)
	case 1:
		return items[0].ID, nil
	default:
		return 0, app.Errorf(app.KindConflict, "external id %q matches %d items; add profile to disambiguate", externalID, len(items))
	}
}
