package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestOffEmitsNothing(t *testing.T) {
	p := Off()

	assert.False(t, p.Enabled())
	assert.Nil(t, p.MetricsHandler())
	assert.Nil(t, p.LogWriter())
	assert.Empty(t, p.LogWriters())
	require.NoError(t, p.Shutdown(t.Context()))
}

// The whole point of registering globally: a package declares an instrument
// against otel.Meter and it reaches this provider's readers with nothing handed
// to it (ADR telemetry-is-exported-over-otlp-with-no-collector-and-the-same-instruments-serve-a-local-scrape).
func TestNewRegistersTheGlobalMeterProvider(t *testing.T) {
	p, err := New(t.Context(), Options{Scrape: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Shutdown(context.WithoutCancel(t.Context())) })

	counter, err := otel.Meter("telemetry-test").Int64Counter("registration.probe")
	require.NoError(t, err)
	counter.Add(t.Context(), 3)

	assert.Contains(t, scrape(t, p.MetricsHandler()), "registration_probe")
}

// Spans have no local sink, so the scrape gate alone must leave the global
// tracer no-op: a span opened with nothing exporting stays non-recording rather
// than accumulating in a provider no reader drains.
func TestScrapeAloneLeavesTheGlobalTracerNoop(t *testing.T) {
	p, err := New(t.Context(), Options{Scrape: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Shutdown(context.WithoutCancel(t.Context())) })

	_, span := otel.Tracer("telemetry-test").Start(t.Context(), "probe")
	span.End()

	assert.False(t, span.SpanContext().IsValid())
}

func TestNewWithBothGatesOffIsOff(t *testing.T) {
	p, err := New(t.Context(), Options{})
	require.NoError(t, err)
	assert.False(t, p.Enabled())
	assert.Nil(t, p.MetricsHandler())
}

func TestNewRejectsUnusableExportConfig(t *testing.T) {
	base := Options{
		Export:   true,
		Endpoint: "https://otlp-gateway-prod-us-central-0.grafana.net/otlp",
		User:     "123456",
		Token:    "glc_secret",
	}

	tests := []struct {
		name string
		edit func(*Options)
		want string
	}{
		{"no endpoint", func(o *Options) { o.Endpoint = "" }, "endpoint is required"},
		{"no user", func(o *Options) { o.User = "" }, "user is required"},
		{"no token", func(o *Options) { o.Token = "" }, "token is required"},
		{"plaintext endpoint", func(o *Options) { o.Endpoint = "http://gateway.example.com/otlp" }, "must use https"},
		{"endpoint has no host", func(o *Options) { o.Endpoint = "https:///otlp" }, "no host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := base
			tt.edit(&opts)

			p, err := New(t.Context(), opts)
			require.Error(t, err)
			assert.Nil(t, p)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// The scrape gate stands alone: no endpoint and no token, so the local debug
// loop works with no account configured.
func TestScrapeWithoutExport(t *testing.T) {
	p, err := New(t.Context(), Options{
		Scrape:      true,
		Version:     "1.4.0-dev.3",
		Environment: "dev",
		Instance:    "worktree-a",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Shutdown(context.WithoutCancel(t.Context())) })

	assert.True(t, p.Enabled())
	require.NotNil(t, p.MetricsHandler())
	assert.Nil(t, p.LogWriter())

	body := scrape(t, p.MetricsHandler())
	assert.Contains(t, body, "go_memory_used_bytes")

	// The resource identity has to reach the exposition, or a query written
	// locally is not the same query run against the backend.
	assert.Contains(t, body, "target_info")
	for _, want := range []string{
		`service_name="hive-desktop"`,
		`service_version="1.4.0-dev.3"`,
		`deployment_environment_name="dev"`,
		`service_instance_id="worktree-a"`,
	} {
		assert.Contains(t, body, want)
	}
}

// An empty `instance` label is worse than no label at all.
func TestEmptyResourceAttributesAreOmitted(t *testing.T) {
	p, err := New(t.Context(), Options{Scrape: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Shutdown(context.WithoutCancel(t.Context())) })

	body := scrape(t, p.MetricsHandler())
	assert.Contains(t, body, `service_name="hive-desktop"`)
	assert.NotContains(t, body, `service_version=""`)
	assert.NotContains(t, body, `deployment_environment_name=""`)
	assert.NotContains(t, body, `service_instance_id=""`)
}

func TestShutdownIsRepeatable(t *testing.T) {
	p, err := New(t.Context(), Options{Scrape: true})
	require.NoError(t, err)

	require.NoError(t, p.Shutdown(t.Context()))
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestSignalURL(t *testing.T) {
	const want = "https://gw.example.com/otlp/v1/traces"
	assert.Equal(t, want, signalURL("https://gw.example.com/otlp", "traces"))
	assert.Equal(t, want, signalURL("https://gw.example.com/otlp/", "traces"))
	assert.Equal(t, want, signalURL("  https://gw.example.com/otlp  ", "traces"))
}

func TestBasicAuth(t *testing.T) {
	// base64("123456:glc_secret")
	assert.Equal(t, "Basic MTIzNDU2OmdsY19zZWNyZXQ=", basicAuth("123456", "glc_secret"))
}

func scrape(t *testing.T, h http.Handler) string {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MetricsPath, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.NotEmpty(t, strings.TrimSpace(body))
	return body
}
