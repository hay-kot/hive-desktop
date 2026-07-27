package httpapi

import (
	"errors"
	"net/http"

	"github.com/hay-kot/httpkit/errchain"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/web"
	"github.com/hay-kot/hive-desktop/internal/web/mid"
)

func (ctrl *Controller) Handler() http.Handler {
	chain := errchain.New(mid.Errors(ctrl.log, mapAppError))

	mux := http.NewServeMux()
	mux.Handle("GET /api/version", web.VersionHandler("hive.desktop.api"))
	mux.HandleFunc("GET /api/status", chain.ToHandlerFunc(ctrl.Status))
	mux.HandleFunc("GET /api/feeds", chain.ToHandlerFunc(ctrl.Feeds))
	mux.HandleFunc("GET /api/inbox", chain.ToHandlerFunc(ctrl.InboxList))
	mux.HandleFunc("GET /api/inbox/events", chain.ToHandlerFunc(ctrl.InboxItemEvents))
	mux.HandleFunc("POST /api/sources/refresh", chain.ToHandlerFunc(ctrl.SourcesRefresh))
	return mid.Logger(ctrl.log, "/api/status", "/api/version")(mux)
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
