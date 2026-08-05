package mcpsrv

import (
	"cmp"
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

const (
	defaultListLimit  = 200
	defaultEventLimit = 50
)

type listFeedsInput struct {
	Profile string `json:"profile" jsonschema:"Profile id to list feeds for; ids come from list_profiles."`
}

type feedsResult struct {
	Feeds []store.FeedInboxCount `json:"feeds"`
}

func (ctrl *Controller) ListFeeds(ctx context.Context, _ *mcp.CallToolRequest, in listFeedsInput) (*mcp.CallToolResult, feedsResult, error) {
	if in.Profile == "" {
		return nil, feedsResult{}, ctrl.toolError(app.Errorf(app.KindInvalid, "profile is required"))
	}
	counts, err := ctrl.core.Inbox.FeedCounts(ctx, in.Profile)
	if err != nil {
		return nil, feedsResult{}, ctrl.toolError(err)
	}
	return nil, feedsResult{Feeds: counts}, nil
}

type listInboxInput struct {
	Profile    string `json:"profile,omitempty"    jsonschema:"Profile id to scope to; required when feed is set."`
	ExternalID string `json:"externalId,omitempty" jsonschema:"A source's own id (e.g. a GitHub node id); returns every matching item across profiles."`
	Feed       string `json:"feed,omitempty"       jsonschema:"Feed id from list_feeds (e.g. hive/desktop-prs) to return only that feed's items."`
	Archived   bool   `json:"archived,omitempty"   jsonschema:"When true, list archived items instead of active ones."`
	Limit      int    `json:"limit,omitempty"      jsonschema:"Maximum items to return; defaults to 200."`
}

// inboxItemView is the store view plus the feed that claims the item, so a flat
// listing can answer "which feed is this in?" — a store.InboxItemView carries no
// feed id of its own.
type inboxItemView struct {
	store.InboxItemView
	FeedID string `json:"feedId"`
}

type itemsResult struct {
	Items []inboxItemView `json:"items"`
}

// The Out type is `any` here and on the other tools answering with store views.
// The SDK infers an output schema from Out, and its inferrer rejects a
// jsonschema struct tag beginning with "WORD=" — which is exactly the shape the
// store views carry for the OpenAPI reflector they were written for. Omitting
// the output schema costs nothing: it is advisory, the content is still the
// same JSON, and the input schemas — the ones a model actually calls through —
// are inferred in full from the adapter-local input structs above.
func (ctrl *Controller) ListInbox(ctx context.Context, _ *mcp.CallToolRequest, in listInboxInput) (*mcp.CallToolResult, any, error) {
	if in.Feed != "" && in.Profile == "" {
		return nil, nil, ctrl.toolError(app.Errorf(app.KindInvalid, "profile is required when feed is set"))
	}
	if in.Limit < 0 {
		return nil, nil, ctrl.toolError(app.Errorf(app.KindInvalid, "limit must be positive"))
	}
	limit := cmp.Or(in.Limit, defaultListLimit)

	var (
		items []store.InboxItemView
		err   error
	)
	switch {
	case in.ExternalID != "":
		items, err = ctrl.core.Inbox.FindItems(ctx, in.Profile, in.ExternalID)
	case in.Feed != "" && in.Archived:
		items, err = ctrl.core.Inbox.ListArchivedInboxItemsByFeed(ctx, in.Profile, in.Feed, limit)
	case in.Feed != "":
		items, err = ctrl.core.Inbox.ListInboxItemsByFeed(ctx, in.Profile, in.Feed, limit)
	default:
		items, err = ctrl.core.Inbox.ListItems(ctx, in.Profile, limit)
	}
	if err != nil {
		return nil, nil, ctrl.toolError(err)
	}

	views, err := ctrl.itemsWithFeed(ctx, items)
	if err != nil {
		return nil, nil, ctrl.toolError(err)
	}
	return nil, itemsResult{Items: views}, nil
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

type listEventsInput struct {
	ItemID     int64  `json:"itemId,omitempty"     jsonschema:"Inbox item id. Provide this or externalId."`
	ExternalID string `json:"externalId,omitempty" jsonschema:"External id resolving to one item; provide this or itemId."`
	Profile    string `json:"profile,omitempty"    jsonschema:"Profile id used to disambiguate an externalId that matches items in more than one profile."`
	Limit      int    `json:"limit,omitempty"      jsonschema:"Maximum events to return; defaults to 50."`
}

type eventsResult struct {
	Events []store.InboxEventView `json:"events"`
}

func (ctrl *Controller) ListInboxItemEvents(ctx context.Context, _ *mcp.CallToolRequest, in listEventsInput) (*mcp.CallToolResult, any, error) {
	if in.ItemID == 0 && in.ExternalID == "" {
		return nil, nil, ctrl.toolError(app.Errorf(app.KindInvalid, "provide itemId or externalId"))
	}
	if in.Limit < 0 {
		return nil, nil, ctrl.toolError(app.Errorf(app.KindInvalid, "limit must be positive"))
	}
	itemID, err := ctrl.resolveItemID(ctx, in)
	if err != nil {
		return nil, nil, ctrl.toolError(err)
	}
	events, err := ctrl.core.Inbox.InboxItemEvents(ctx, itemID, cmp.Or(in.Limit, defaultEventLimit))
	if err != nil {
		return nil, nil, ctrl.toolError(err)
	}
	return nil, eventsResult{Events: events}, nil
}

// resolveItemID accepts an explicit itemId, or an externalId that must resolve
// to exactly one item.
func (ctrl *Controller) resolveItemID(ctx context.Context, in listEventsInput) (int64, error) {
	if in.ItemID != 0 {
		return in.ItemID, nil
	}
	items, err := ctrl.core.Inbox.FindItems(ctx, in.Profile, in.ExternalID)
	if err != nil {
		return 0, err
	}
	switch len(items) {
	case 0:
		return 0, app.Errorf(app.KindNotFound, "no item with external id %q", in.ExternalID)
	case 1:
		return items[0].ID, nil
	default:
		return 0, app.Errorf(app.KindConflict, "external id %q matches %d items; add profile to disambiguate", in.ExternalID, len(items))
	}
}
