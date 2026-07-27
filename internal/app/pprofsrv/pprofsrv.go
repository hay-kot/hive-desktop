// Package pprofsrv serves Go's net/http/pprof handlers on a dedicated loopback
// listener, gated by development.pprof and off by default. It is owned by
// App's lifecycle rather than mounted on the always-on HTTP server — ADR 0023.
package pprofsrv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Server is the pprof debug endpoint: a loopback-only HTTP server exposing the
// /debug/pprof/ handlers.
type Server struct {
	host   string
	port   int
	logger zerolog.Logger

	server   *http.Server
	listener net.Listener

	stopOnce sync.Once
}

// New builds a server bound to host:port at Start.
func New(host string, port int, logger zerolog.Logger) *Server {
	return &Server{host: host, port: port, logger: logger}
}

// Start binds the configured loopback host and serves in a goroutine,
// returning any bind error for the caller to handle.
func (s *Server) Start(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return fmt.Errorf("pprof endpoint: %w", err)
	}
	s.listener = ln
	s.server = &http.Server{Handler: handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error().Err(err).Msg("pprof endpoint stopped unexpectedly")
		}
	}()
	s.logger.Info().Str("host", s.Host()).Int("port", s.Port()).Msg("pprof endpoint started")
	return nil
}

// Stop gracefully shuts the server down until ctx is done. Idempotent: a
// second call, or a call when Start never ran or never bound, is a no-op
// returning nil.
func (s *Server) Stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		if s.server == nil {
			return
		}
		err = s.server.Shutdown(ctx)
	})
	return err
}

// Running reports whether Start bound successfully.
func (s *Server) Running() bool { return s.listener != nil }

// Port returns the bound TCP port once Running, else the configured port.
func (s *Server) Port() int {
	if s.listener != nil {
		if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
			return addr.Port
		}
	}
	return s.port
}

// Host returns the actual bound address once Running, else the configured host.
func (s *Server) Host() string {
	if s.listener != nil {
		if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
			return addr.IP.String()
		}
	}
	return s.host
}

// handler mounts net/http/pprof on a private mux, not http.DefaultServeMux, so
// these handlers are reachable only from this endpoint.
func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
