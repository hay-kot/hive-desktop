// Package httpapi is the agent-facing HTTP adapter over app.App: a loopback
// read + reload surface for observing the pipeline without reading SQLite. It
// mounts onto the webhook listener's loopback server (ADR 0019).
package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/webtools"
)

// PathPrefix is where App mounts this handler on the webhook listener.
const PathPrefix = "/api/"

const defaultListLimit = 200

type Server struct {
	core *app.App
}

func New(core *app.App) *Server { return &Server{core: core} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/version", webtools.VersionHandler("hive desktop agent API"))
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/feeds", s.handleFeeds)
	mux.HandleFunc("GET /api/inbox", s.handleInbox)
	mux.HandleFunc("GET /api/inbox/events", s.handleItemEvents)
	mux.HandleFunc("POST /api/sources/refresh", s.handleRefresh)
	return mux
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profile := q.Get("profile")
	ctx := r.Context()

	switch {
	case q.Get("externalId") != "":
		items, err := s.core.Inbox.FindItems(ctx, profile, q.Get("externalId"))
		s.writeItems(w, items, err)
	case q.Get("feed") != "":
		if profile == "" {
			s.writeError(w, app.Errorf(app.KindInvalid, "profile is required when feed is set"))
			return
		}
		limit := queryInt(q.Get("limit"), defaultListLimit)
		if q.Get("archived") == "true" {
			items, err := s.core.Inbox.ListArchivedInboxItemsByFeed(ctx, profile, q.Get("feed"), limit)
			s.writeItems(w, items, err)
			return
		}
		items, err := s.core.Inbox.ListInboxItemsByFeed(ctx, profile, q.Get("feed"), limit)
		s.writeItems(w, items, err)
	default:
		items, err := s.core.Inbox.ListItems(ctx, profile, queryInt(q.Get("limit"), defaultListLimit))
		s.writeItems(w, items, err)
	}
}

func (s *Server) handleItemEvents(w http.ResponseWriter, r *http.Request) {
	itemID, err := s.resolveItemID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	events, err := s.core.Inbox.InboxItemEvents(r.Context(), itemID, queryInt(r.URL.Query().Get("limit"), 50))
	if err != nil {
		s.writeError(w, err)
		return
	}
	webtools.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

// resolveItemID accepts an explicit itemId, or an externalId that must resolve
// to exactly one item.
func (s *Server) resolveItemID(r *http.Request) (int64, error) {
	q := r.URL.Query()
	if raw := q.Get("itemId"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, app.Errorf(app.KindInvalid, "itemId must be an integer")
		}
		return id, nil
	}
	external := q.Get("externalId")
	if external == "" {
		return 0, app.Errorf(app.KindInvalid, "itemId or externalId is required")
	}
	items, err := s.core.Inbox.FindItems(r.Context(), q.Get("profile"), external)
	if err != nil {
		return 0, err
	}
	switch len(items) {
	case 0:
		return 0, app.Errorf(app.KindNotFound, "no item with external id %q", external)
	case 1:
		return items[0].ID, nil
	default:
		return 0, app.Errorf(app.KindConflict, "external id %q matches %d items; add profile to disambiguate", external, len(items))
	}
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	profile := r.URL.Query().Get("profile")
	if profile == "" {
		s.writeError(w, app.Errorf(app.KindInvalid, "profile is required"))
		return
	}
	counts, err := s.core.Inbox.FeedCounts(r.Context(), profile)
	if err != nil {
		s.writeError(w, err)
		return
	}
	webtools.WriteJSON(w, http.StatusOK, map[string]any{"feeds": counts})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	sum, err := s.core.RefreshSources(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	webtools.WriteJSON(w, http.StatusOK, map[string]any{
		"sources": sum.Sources, "appended": sum.Appended, "failed": sum.Failed,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	running, port := s.core.Webhooks.Endpoint(r.Context())
	webtools.WriteJSON(w, http.StatusOK, map[string]any{
		"webhook": map[string]any{
			"running":    running,
			"host":       s.core.Webhooks.Host(),
			"port":       port,
			"pathPrefix": webhook.PathPrefix,
		},
		"api": map[string]any{"pathPrefix": PathPrefix},
	})
}

func (s *Server) writeItems(w http.ResponseWriter, items []store.InboxItemView, err error) {
	if err != nil {
		s.writeError(w, err)
		return
	}
	webtools.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func queryInt(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// writeError maps a core error's Kind to an HTTP status once. An *app.Error
// marshals to {kind, message}; anything else is an opaque 500.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	if appErr, ok := errors.AsType[*app.Error](err); ok {
		webtools.WriteJSON(w, statusForKind(appErr.Kind), appErr)
		return
	}
	webtools.WriteJSON(w, http.StatusInternalServerError, map[string]string{
		"kind": string(app.KindInternal), "message": "internal error",
	})
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
