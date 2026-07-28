package httpapi

import (
	"errors"
	"net/http"

	"github.com/hay-kot/httpkit/errchain"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web"
	"github.com/hay-kot/hive-desktop/internal/web/mid"
)

// RawBinary marks a request or response body as raw bytes of one of the given
// media types rather than JSON. Note is a one-line human description surfaced
// in both the route index and the OpenAPI requestBody.
type RawBinary struct {
	Media []string
	Note  string
}

// Op is one HTTP operation. The operations table is the single source the mux,
// the GET /api index, and the OpenAPI document are all built from, so none can
// drift. Request and Response are each a zero-value struct (a JSON body,
// reflected into a schema), a RawBinary, or nil (no body).
type Op struct {
	Method   string
	Path     string
	Summary  string
	Query    any // struct whose `schema`-tagged fields become query parameters
	Request  any
	Response any
	Status   int // success status; 0 means 200
	Handler  errchain.HandlerFunc
}

func (op Op) successStatus() int {
	if op.Status == 0 {
		return http.StatusOK
	}
	return op.Status
}

func (ctrl *Controller) operations() []Op {
	return []Op{
		{
			Method: "GET", Path: "/api", Summary: "List every route this API serves, with a link to the OpenAPI document.",
			Response: apiIndex{}, Handler: ctrl.APIIndex,
		},
		{
			Method: "GET", Path: openAPIPath, Summary: "The OpenAPI description of this API; servers are set to the address it was fetched from.",
			Handler: ctrl.OpenAPI,
		},
		{
			Method: "GET", Path: "/api/version", Summary: "Report the running build's VCS identity.",
			Response: struct {
				Service string    `json:"service"`
				Build   web.Build `json:"build"`
			}{}, Handler: plain(ctrl.version),
		},
		{
			Method: "GET", Path: "/api/status", Summary: "Report whether the webhook listener is running and on which host and port.",
			Response: statusResponse{}, Handler: ctrl.Status,
		},
		{
			Method: "GET", Path: "/api/feeds", Summary: "List a profile's feeds with unread and archived counts.",
			Query: FeedsQuery{}, Response: feedsResponse{}, Handler: ctrl.Feeds,
		},
		{
			Method: "GET", Path: "/api/inbox", Summary: "List a profile's inbox items, optionally filtered by feed or external id.",
			Query: InboxQuery{}, Response: itemsResponse{}, Handler: ctrl.InboxList,
		},
		{
			Method: "GET", Path: "/api/inbox/events", Summary: "List one inbox item's lifecycle events, resolved by itemId or a unique externalId.",
			Query: EventsQuery{}, Response: eventsResponse{}, Handler: ctrl.InboxItemEvents,
		},
		{
			Method: "POST", Path: "/api/sources/refresh", Summary: "Force one producer tick, dropping fetch caches, and report what each source appended.",
			Response: refreshResponse{}, Handler: ctrl.SourcesRefresh,
		},
		{
			Method: "GET", Path: "/api/profiles", Summary: "List every profile with its load status and whether it has an avatar.",
			Response: profilesResponse{}, Handler: ctrl.Profiles,
		},
		{
			Method: "POST", Path: "/api/profiles", Summary: "Create a profile, seeded with the starter graph when exactly one GitHub account is connected.",
			Request: createProfileRequest{}, Response: profileView{}, Status: http.StatusCreated, Handler: ctrl.CreateProfile,
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}", Summary: "Delete a profile, its flow files, its avatar, and its inbox state.",
			Status: http.StatusNoContent, Handler: ctrl.DeleteProfile,
		},
		{
			Method: "GET", Path: "/api/profiles/{id}/image", Summary: "Return a profile's avatar as a 128x128 PNG, or 404 when it has none.",
			Response: RawBinary{Media: []string{"image/png"}}, Handler: ctrl.GetProfileImage,
		},
		{
			Method: "PUT", Path: "/api/profiles/{id}/image", Summary: "Set a profile's avatar from the raw request body; the image is normalized to a 128x128 PNG.",
			Request: RawBinary{
				Media: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				Note:  "Send the image as the raw request body (PNG, JPEG, GIF, or WebP) — not multipart/form-data.",
			}, Response: profileView{}, Handler: ctrl.SetProfileImage,
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}/image", Summary: "Clear a profile's avatar so its rail reverts to the letter chip.",
			Response: profileView{}, Handler: ctrl.ClearProfileImage,
		},
	}
}

func (ctrl *Controller) Handler() http.Handler {
	chain := errchain.New(mid.Errors(ctrl.log, mapAppError))

	mux := http.NewServeMux()
	for _, op := range ctrl.operations() {
		mux.HandleFunc(op.Method+" "+op.Path, chain.ToHandlerFunc(op.Handler))
	}
	return mid.Logger(ctrl.log, "/api/status", "/api/version")(mux)
}

// plain adapts a pre-built handler (the shared version handler) to the error
// chain; it writes its own response and never fails.
func plain(h http.HandlerFunc) errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		h(w, r)
		return nil
	}
}

// mapAppError maps a core error's Kind to an HTTP status exactly once; an
// *app.Error marshals to {kind, message}.
func mapAppError(err error) (int, any, bool) {
	appErr, ok := errors.AsType[*app.Error](err)
	if !ok {
		return 0, nil, false
	}
	return statusForKind(appErr.Kind), appErr, true
}

func statusForKind(k app.Kind) int {
	switch k {
	case app.KindInvalid:
		return http.StatusBadRequest
	case app.KindNotFound:
		return http.StatusNotFound
	case app.KindConflict:
		return http.StatusConflict
	case app.KindUnauthenticated:
		return http.StatusUnauthorized
	case app.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
