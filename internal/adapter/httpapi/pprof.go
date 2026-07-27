package httpapi

import (
	"net/http"
	"net/http/pprof"
)

// PprofPathPrefix is where PprofHandler mounts on the shared loopback server.
const PprofPathPrefix = "/debug/pprof/"

// PprofHandler serves net/http/pprof on a private mux, not the
// http.DefaultServeMux that importing the package writes to.
func PprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
