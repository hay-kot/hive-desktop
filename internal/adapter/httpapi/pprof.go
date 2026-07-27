package httpapi

import (
	"net/http"
	"net/http/pprof"
)

// PprofPathPrefix is where the pprof handler mounts on the shared loopback
// server when development.pprof is enabled.
const PprofPathPrefix = "/debug/pprof/"

// PprofHandler serves Go's net/http/pprof handlers. It is mounted on the shared
// loopback server only when development.pprof is enabled (ADR 0023), so the
// handlers are never reachable by default. Registered on a private mux, not the
// http.DefaultServeMux that importing net/http/pprof writes to.
func PprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
