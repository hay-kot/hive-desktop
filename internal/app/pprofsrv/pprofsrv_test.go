package pprofsrv

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerStartServesPprofAndStops(t *testing.T) {
	s := New("127.0.0.1", 0, zerolog.Nop())
	require.NoError(t, s.Start(t.Context()))

	require.True(t, s.Running())
	require.Positive(t, s.Port())
	assert.Equal(t, "127.0.0.1", s.Host())

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/goroutine?debug=1", s.Port()))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, s.Stop(t.Context()))

	resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/", s.Port()))
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "endpoint must be unreachable after Stop")
}

func TestServerStopWithoutStartIsNoop(t *testing.T) {
	s := New("127.0.0.1", 0, zerolog.Nop())
	require.NoError(t, s.Stop(t.Context()))
	assert.False(t, s.Running())
}

func TestServerStopIsIdempotent(t *testing.T) {
	s := New("127.0.0.1", 0, zerolog.Nop())
	require.NoError(t, s.Start(t.Context()))

	require.NoError(t, s.Stop(t.Context()))
	require.NoError(t, s.Stop(t.Context()))
}

func TestServerStopIsConcurrencySafe(t *testing.T) {
	s := New("127.0.0.1", 0, zerolog.Nop())
	require.NoError(t, s.Start(t.Context()))

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.Stop(t.Context())
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoError(t, err)
	}
}

func TestServerStartBindFailureIsReported(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port

	s := New("127.0.0.1", port, zerolog.Nop())
	err = s.Start(t.Context())
	require.Error(t, err)
	assert.False(t, s.Running())
	assert.Equal(t, port, s.Port(), "reports the configured port when it never bound")
}
