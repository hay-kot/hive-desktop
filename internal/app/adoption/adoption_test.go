package adoption

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReporterCapturesOneAnonymousEventPerUTCDay(t *testing.T) {
	var requests atomic.Int32
	var bodies []map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/i/v0/e/", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, time.September, 23, 23, 30, 0, 0, time.FixedZone("local", -5*60*60))
	stateDir := t.TempDir()
	reporter, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     server.URL,
		StateDir:     stateDir,
		Version:      "1.2.3",
		Channel:      "stable",
		Enabled:      true,
	}, zerolog.Nop())
	require.NoError(t, err)
	reporter.client = server.Client()
	reporter.now = func() time.Time { return now }

	require.NoError(t, reporter.reportIfDue(t.Context()))
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(1), requests.Load())

	body := bodies[0]
	assert.Len(t, body, 5)
	assert.Equal(t, "phc_project", body["api_key"])
	assert.Equal(t, eventName, body["event"])
	assert.Equal(t, now.UTC().Format(time.RFC3339), body["timestamp"])
	assert.NotEmpty(t, body["distinct_id"])
	properties, ok := body["properties"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, properties, 6)
	assert.Equal(t, false, properties["$process_person_profile"])
	assert.Equal(t, true, properties["$geoip_disable"])
	assert.Equal(t, "1.2.3", properties["app_version"])
	assert.Equal(t, "stable", properties["release_channel"])
	assert.Equal(t, runtime.GOOS, properties["os"])
	assert.Equal(t, runtime.GOARCH, properties["arch"])

	stateRaw, err := os.ReadFile(filepath.Join(stateDir, stateFileName))
	require.NoError(t, err)
	var state map[string]any
	require.NoError(t, json.Unmarshal(stateRaw, &state))
	assert.Len(t, state, 2)
	assert.Equal(t, body["distinct_id"], state["installation_id"])
	assert.Equal(t, "2026-09-24", state["last_active_date"])

	now = now.Add(24 * time.Hour)
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(2), requests.Load())

	restarted, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     server.URL,
		StateDir:     stateDir,
		Version:      "1.2.3",
		Channel:      "stable",
		Enabled:      true,
	}, zerolog.Nop())
	require.NoError(t, err)
	restarted.client = server.Client()
	restarted.now = func() time.Time { return now }
	require.NoError(t, restarted.reportIfDue(t.Context()))
	assert.Equal(t, int32(2), requests.Load())
}

func TestReporterRetriesAfterFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     server.URL,
		StateDir:     t.TempDir(),
		Enabled:      true,
	}, zerolog.Nop())
	require.NoError(t, err)
	reporter.client = server.Client()

	require.Error(t, reporter.reportIfDue(t.Context()))
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(2), requests.Load())
}

func TestReporterAppliesOptOutWithoutRestart(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stateDir := t.TempDir()
	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.UTC)
	reporter, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     server.URL,
		StateDir:     stateDir,
		Enabled:      false,
	}, zerolog.Nop())
	require.NoError(t, err)
	reporter.client = server.Client()
	reporter.now = func() time.Time { return now }

	assert.True(t, reporter.Configured())
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(0), requests.Load())
	_, err = os.Stat(filepath.Join(stateDir, stateFileName))
	require.ErrorIs(t, err, os.ErrNotExist)

	reporter.SetEnabled(true)
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(1), requests.Load())

	now = now.Add(24 * time.Hour)
	reporter.SetEnabled(false)
	require.NoError(t, reporter.reportIfDue(t.Context()))
	assert.Equal(t, int32(1), requests.Load())
}

func TestOptOutCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()

	reporter, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     server.URL,
		StateDir:     t.TempDir(),
		Enabled:      true,
	}, zerolog.Nop())
	require.NoError(t, err)
	reporter.client = server.Client()

	result := make(chan error, 1)
	go func() { result <- reporter.reportIfDue(t.Context()) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not start")
	}

	reporter.SetEnabled(false)
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("capture was not cancelled")
	}
	close(release)
}

func TestDisabledReporterCreatesNoState(t *testing.T) {
	stateDir := t.TempDir()
	reporter, err := New(Options{StateDir: stateDir}, zerolog.Nop())
	require.NoError(t, err)

	reporter.Start(t.Context())
	require.NoError(t, reporter.Stop(t.Context()))
	_, err = os.Stat(filepath.Join(stateDir, stateFileName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNewRejectsNonHTTPSEndpoint(t *testing.T) {
	_, err := New(Options{
		ProjectToken: "phc_project",
		Endpoint:     "http://posthog.example",
		StateDir:     t.TempDir(),
	}, zerolog.Nop())
	require.EqualError(t, err, "adoption endpoint must be an https origin")
}
