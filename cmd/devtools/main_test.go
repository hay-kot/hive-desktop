package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/hay-kot/hive-desktop/cmd/internal/devproxy"
)

func TestCommandRejectsInvalidLogLevel(t *testing.T) {
	logger := zerolog.Nop()
	err := newDevtoolsCommand(&logger).Run(context.Background(), []string{"devtools", "--log-level", "chatty", "prepare"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown Level String")
}

func TestCommandRejectsPositionalArguments(t *testing.T) {
	logger := zerolog.Nop()
	err := newDevtoolsCommand(&logger).Run(context.Background(), []string{"devtools", "prepare", "unexpected"})
	require.Error(t, err)
	var exit cli.ExitCoder
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 2, exit.ExitCode())
}

func runCheckProxy(t *testing.T, args ...string) error {
	t.Helper()
	logger := zerolog.Nop()
	return newDevtoolsCommand(&logger).Run(t.Context(), append([]string{"devtools", "check-proxy"}, args...))
}

// An empty override is the opt-out (overrides.env), and it must pass without
// probing anything — otherwise opting out of the proxy would still require one.
func TestCheckProxyPassesWhenNotProxied(t *testing.T) {
	t.Setenv(devproxy.EnvAPIBase, "")
	require.NoError(t, runCheckProxy(t))
}

func TestCheckProxyPassesWhenProxyAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devserver":true}`))
	}))
	defer server.Close()

	t.Setenv(devproxy.EnvAPIBase, server.URL)
	require.NoError(t, runCheckProxy(t))
}

// The default is a single probe: someone running desktop:dev by hand wants to be
// told immediately rather than waiting on a proxy they forgot to start.
func TestCheckProxyFailsFastWithoutWait(t *testing.T) {
	t.Setenv(devproxy.EnvAPIBase, "http://127.0.0.1:1")
	err := runCheckProxy(t)
	require.Error(t, err)
	var exit cli.ExitCoder
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 1, exit.ExitCode())
}

// --wait is what makes the .solo.yml layout deterministic: the proxy tab and the
// desktop tab start together, and `go run ./cmd/devserver` has to link before it
// binds, so the port is briefly unreachable.
func TestCheckProxyWaitToleratesLateProxy(t *testing.T) {
	// Claim a port, release it, then hand it to a real server after a delay.
	// That is the shape of the race: refused, then answering.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := probe.Addr().String()
	require.NoError(t, probe.Close())

	t.Setenv(devproxy.EnvAPIBase, "http://"+addr)

	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devserver":true}`))
		}),
		ReadHeaderTimeout: time.Second,
	}
	defer server.Close() //nolint:errcheck // test teardown
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = server.ListenAndServe()
	}()

	require.NoError(t, runCheckProxy(t, "--wait", "30s"))
}

// The wait is bounded: a proxy that never arrives still fails, rather than
// hanging a dev session forever.
func TestCheckProxyWaitGivesUp(t *testing.T) {
	t.Setenv(devproxy.EnvAPIBase, "http://127.0.0.1:1")
	start := time.Now()
	require.Error(t, runCheckProxy(t, "--wait", "1s"))
	assert.GreaterOrEqual(t, time.Since(start), time.Second, "must retry for the full wait")
}
