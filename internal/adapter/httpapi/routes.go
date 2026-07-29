package httpapi

import (
	"errors"
	"net/http"
	"strings"

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
	Status   int       // success status; 0 means 200
	Errors   []ErrResp // documented non-success statuses beyond the generic default
	Handler  errchain.HandlerFunc
}

// ErrResp documents one non-success status an operation can return, and when.
// The error body is always the shared {kind, message, fields?} shape.
type ErrResp struct {
	Status int
	When   string
}

func (op Op) successStatus() int {
	if op.Status == 0 {
		return http.StatusOK
	}
	return op.Status
}

// pattern is the ServeMux pattern this op registers under. A path ending in "/"
// is anchored with {$} so it matches only that exact path: the index lives at
// the mount root /api/ (a request for /api is redirected there by the listener
// that mounts this handler), and without the anchor it would become a subtree
// that swallows every otherwise-unmatched /api/… request instead of 404ing.
func (op Op) pattern() string {
	path := op.Path
	if strings.HasSuffix(path, "/") {
		path += "{$}"
	}
	return op.Method + " " + path
}

func (ctrl *Controller) operations() []Op {
	ops := ctrl.baseOperations()
	// No token means terminal mode is off for this run (experimental.terminal,
	// ADR 0033): the routes are absent rather than answering 503, so the route
	// index and OpenAPI document never advertise a surface that cannot work.
	if ctrl.terminalToken != "" {
		ops = append(ops, ctrl.terminalOperations()...)
	}
	return ops
}

func (ctrl *Controller) baseOperations() []Op {
	return []Op{
		{
			Method: "GET", Path: "/api/", Summary: "List every route this API serves, with a link to the OpenAPI document.",
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
			Errors: []ErrResp{{Status: 422, When: "the query failed validation (profile is required)"}},
		},
		{
			Method: "GET", Path: "/api/inbox", Summary: "List a profile's inbox items, optionally filtered by feed or external id; each item carries a feedId (the claiming feed, empty when unrouted) and a payload of the source's raw JSON.",
			Query: InboxQuery{}, Response: itemsResponse{}, Handler: ctrl.InboxList,
			Errors: []ErrResp{{Status: 422, When: "the query failed validation (profile is required when feed is set)"}},
		},
		{
			Method: "GET", Path: "/api/inbox/events", Summary: "List one inbox item's lifecycle events, resolved by itemId or a unique externalId; each event's detail is source-specific raw JSON.",
			Query: EventsQuery{}, Response: eventsResponse{}, Handler: ctrl.InboxItemEvents,
			Errors: []ErrResp{
				{Status: 422, When: "neither itemId nor externalId was given"},
				{Status: 404, When: "no item matches the externalId"},
				{Status: 409, When: "the externalId matches items in more than one profile; add profile to disambiguate"},
			},
		},
		{
			Method: "POST", Path: "/api/sources/refresh", Summary: "Force one producer tick across all sources, dropping fetch caches; returns aggregate totals, not a per-source breakdown.",
			Response: refreshResponse{}, Handler: ctrl.SourcesRefresh,
			Errors: []ErrResp{{Status: 503, When: "no producer is available (e.g. mock mode)"}},
		},
		{
			Method: "GET", Path: "/api/profiles", Summary: "List every profile with its load status and whether it has an avatar.",
			Response: profilesResponse{}, Handler: ctrl.Profiles,
		},
		{
			Method: "POST", Path: "/api/profiles", Summary: "Create a profile, seeded with the starter graph when exactly one GitHub account is connected.",
			Request: createProfileRequest{}, Response: profileView{}, Status: http.StatusCreated, Handler: ctrl.CreateProfile,
			Errors: []ErrResp{{Status: 422, When: "name is missing or invalid"}},
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}", Summary: "Delete a profile, its flow files, its avatar, and its inbox state.",
			Status: http.StatusNoContent, Handler: ctrl.DeleteProfile,
		},
		{
			Method: "GET", Path: "/api/profiles/{id}/image", Summary: "Return a profile's avatar as a 128x128 PNG, or 404 when it has none.",
			Response: RawBinary{Media: []string{"image/png"}}, Handler: ctrl.GetProfileImage,
			Errors: []ErrResp{{Status: 404, When: "the profile has no image, or no such profile"}},
		},
		{
			Method: "PUT", Path: "/api/profiles/{id}/image", Summary: "Set a profile's avatar from the raw request body; the image is normalized to a 128x128 PNG.",
			Request: RawBinary{
				Media: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				Note:  "Send the image as the raw request body (PNG, JPEG, GIF, or WebP) — not multipart/form-data.",
			}, Response: profileView{}, Handler: ctrl.SetProfileImage,
			Errors: []ErrResp{
				{Status: 400, When: "the body was unreadable or not a supported image"},
				{Status: 404, When: "no such profile"},
			},
		},
		{
			Method: "DELETE", Path: "/api/profiles/{id}/image", Summary: "Clear a profile's avatar so its rail reverts to the letter chip.",
			Response: profileView{}, Handler: ctrl.ClearProfileImage,
		},
		{
			Method: "GET", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Return a webhook source node's feed-mark image as a 128x128 PNG, or 404 when it has none.",
			Response: RawBinary{Media: []string{"image/png"}}, Handler: ctrl.GetNodeImage,
			Errors: []ErrResp{{Status: 404, When: "the node has no image, or no such flow or node"}},
		},
		{
			Method: "PUT", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Set a webhook source node's feed-mark image from the raw request body; it is normalized to a 128x128 PNG and shown on the source's items instead of its icon.",
			Request: RawBinary{
				Media: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				Note:  "Send the image as the raw request body (PNG, JPEG, GIF, or WebP) — not multipart/form-data.",
			}, Response: nodeImageView{}, Handler: ctrl.SetNodeImage,
			Errors: []ErrResp{
				{Status: 400, When: "the body was unreadable or not a supported image, or the node is not a webhook source"},
				{Status: 404, When: "no such flow or node"},
			},
		},
		{
			Method: "DELETE", Path: "/api/flows/{flowId}/nodes/{nodeId}/image", Summary: "Clear a webhook source node's feed-mark image so it reverts to its icon.",
			Response: nodeImageView{}, Handler: ctrl.ClearNodeImage,
			Errors: []ErrResp{{Status: 404, When: "no such flow or node"}},
		},
	}
}

func (ctrl *Controller) terminalOperations() []Op {
	return []Op{
		{
			Method: "POST", Path: "/api/terminal/attach", Summary: "Attach a tmux control-mode client to a session slug and return its windows plus the WebSocket path the data plane is served on.",
			Request: terminalSizeRequest{}, Response: terminalAttachResponse{}, Handler: ctrl.TerminalAttach,
			Errors: terminalErrors("the slug names no reachable tmux session"),
		},
		{
			Method: "POST", Path: "/api/terminal/resize", Summary: "Resize the attached control client; tmux gives every client of a window the same size and the smallest wins.",
			Request: terminalSizeRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalResize,
			Errors: terminalErrors("no terminal is attached for that slug"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/new", Summary: "Create a window in the attached session and return its tmux window id.",
			Request: terminalSlugRequest{}, Response: terminalNewWindowResponse{}, Handler: ctrl.TerminalNewWindow,
			Errors: terminalErrors("no terminal is attached for that slug"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/close", Summary: "Kill one window of the attached session.",
			Request: terminalWindowRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalCloseWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/rename", Summary: "Rename one window of the attached session.",
			Request: terminalRenameRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalRenameWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/windows/select", Summary: "Make one window the attached session's active window.",
			Request: terminalWindowRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalSelectWindow,
			Errors: terminalErrors("no terminal is attached for that slug, or no such window"),
		},
		{
			Method: "POST", Path: "/api/terminal/detach", Summary: "Close the control client, leaving the tmux session itself running.",
			Request: terminalSlugRequest{}, Status: http.StatusNoContent, Handler: ctrl.TerminalDetach,
			Errors: terminalErrors("no terminal is attached for that slug"),
		},
	}
}

// terminalErrors documents what every terminal operation can answer beyond the
// generic error: the bearer token these — and only these — routes require, and
// tmux being absent or too old.
func terminalErrors(notFound string) []ErrResp {
	return []ErrResp{
		{Status: 401, When: "the Authorization: Bearer token is missing or wrong"},
		{Status: 404, When: notFound},
		{Status: 503, When: "tmux is unavailable: missing, older than 3.2, or an unsupported build"},
	}
}

func (ctrl *Controller) Handler() http.Handler {
	chain := errchain.New(mid.Errors(ctrl.log, mapAppError))

	mux := http.NewServeMux()
	preflighted := map[string]bool{}
	for _, op := range ctrl.operations() {
		handler := chain.ToHandlerFunc(op.Handler)
		if strings.HasPrefix(op.Path, TerminalPathPrefix) {
			handler = ctrl.cors.wrap(handler)
			if !preflighted[op.Path] {
				preflighted[op.Path] = true
				mux.HandleFunc("OPTIONS "+op.Path, ctrl.cors.preflight)
			}
		}
		mux.HandleFunc(op.pattern(), handler)
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
