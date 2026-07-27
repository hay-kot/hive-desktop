// Package httpapi is the agent-facing HTTP adapter over app.App: a loopback
// read + reload surface a test harness drives to observe the pipeline's
// conclusions without reading SQLite. It is a driving adapter — it holds
// *app.App, maps app.Kind to HTTP status once, and keeps no logic of its own.
//
// It mounts onto the webhook listener's loopback server (ADR 0019) rather than
// binding a second port, so the same port serves webhook push and this API.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// PathPrefix is where App mounts this handler on the webhook listener.
const PathPrefix = "/api/"

const defaultListLimit = 200

// Server adapts app.App to HTTP. Build it with New and mount Handler().
type Server struct {
	core *app.App
}

func New(core *app.App) *Server { return &Server{core: core} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/help", s.handleHelp)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/feeds", s.handleFeeds)
	mux.HandleFunc("GET /api/inbox", s.handleInbox)
	mux.HandleFunc("GET /api/inbox/events", s.handleItemEvents)
	mux.HandleFunc("POST /api/sources/refresh", s.handleRefresh)
	return mux
}

type endpointDoc struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

var apiEndpoints = []endpointDoc{
	{"GET", "/api/help", "This contract: endpoints and the observe→reload→read→retry loop."},
	{"GET", "/api/status", "Build identity plus the webhook listener's host/port/pathPrefix (the port this API shares)."},
	{"GET", "/api/feeds?profile=", "Per-feed item counts for a profile."},
	{"GET", "/api/inbox?profile=&feed=&externalId=&archived=&limit=", "Inbox items: all (default), one feed, or by external id. Payload carries repo/num/reason/state."},
	{"GET", "/api/inbox/events?itemId=|externalId=&profile=&limit=", "One item's event history."},
	{"POST", "/api/sources/refresh", "Drop fetch caches and re-poll every pull source once; returns after the log is appended."},
}

var apiNotes = []string{
	"There is no server-side wait: drive the event, POST /api/sources/refresh, then read — and retry the read a few times, because the flow engine commits on its own goroutine a moment after the log is appended.",
	"Match an item by its payload's repo/num/reason; external_id is the source's own id (a GitHub global node id), not repo#num.",
	"Webhook deliveries do not go through refresh — push via the devserver, then read and retry.",
	"This API rides the webhook listener's loopback port, so it is only up when webhooks are enabled.",
}

func (s *Server) handleHelp(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":   "hive desktop agent API",
		"overview":  "Observe the notification pipeline's conclusions and force a re-poll, so a test asserts against a supported surface instead of SQLite.",
		"endpoints": apiEndpoints,
		"notes":     apiNotes,
	})
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
		limit := queryInt(q.Get("limit"), defaultListLimit)
		var (
			items []store.InboxItemView
			err   error
		)
		if q.Get("archived") == "true" {
			items, err = s.core.Inbox.ListArchivedInboxItemsByFeed(ctx, profile, q.Get("feed"), limit)
		} else {
			items, err = s.core.Inbox.ListInboxItemsByFeed(ctx, profile, q.Get("feed"), limit)
		}
		s.writeItems(w, items, err)
	default:
		items, err := s.core.Inbox.ListItems(ctx, profile, queryInt(q.Get("limit"), defaultListLimit))
		s.writeItems(w, items, err)
	}
}

func (s *Server) handleItemEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()
	limit := queryInt(q.Get("limit"), 50)

	itemID, err := s.resolveItemID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	events, err := s.core.Inbox.InboxItemEvents(ctx, itemID, limit)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
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
	writeJSON(w, http.StatusOK, map[string]any{"feeds": counts})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RefreshSources(r.Context()); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	running, port := s.core.Webhooks.Endpoint(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"build": buildInfo(),
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
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func buildInfo() map[string]any {
	info := map[string]any{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info["go"] = bi.GoVersion
		for _, kv := range bi.Settings {
			switch kv.Key {
			case "vcs.revision":
				info["revision"] = kv.Value
			case "vcs.modified":
				info["modified"] = kv.Value == "true"
			case "vcs.time":
				info["time"] = kv.Value
			}
		}
	}
	return info
}

func queryInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError maps a core error's Kind to an HTTP status exactly once. An
// *app.Error marshals to {kind, message}; anything else is an opaque 500.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := statusForKind(app.KindOf(err))
	if appErr := asAppError(err); appErr != nil {
		writeJSON(w, status, appErr)
		return
	}
	writeJSON(w, status, map[string]string{"kind": string(app.KindInternal), "message": "internal error"})
}

func asAppError(err error) *app.Error {
	var e *app.Error
	if errors.As(err, &e) {
		return e
	}
	return nil
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
