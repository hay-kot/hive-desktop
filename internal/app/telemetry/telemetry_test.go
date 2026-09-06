package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOffEmitsNothing(t *testing.T) {
	p := Off()

	assert.False(t, p.Enabled())
	assert.Nil(t, p.MetricsHandler())
	assert.Nil(t, p.LogWriter())
	assert.Empty(t, p.LogWriters())
	require.NoError(t, p.Shutdown(t.Context()))

	// The tracer is usable rather than nil, so a span is safe to open without
	// checking whether telemetry is configured.
	require.NotNil(t, p.Tracer())
	_, span := p.Tracer().Start(t.Context(), "noop")
	span.End()
}

func TestNewWithBothGatesOffIsOff(t *testing.T) {
	p, err := New(t.Context(), Options{})
	require.NoError(t, err)
	assert.False(t, p.Enabled())
	assert.Nil(t, p.MetricsHandler())
}

// An endpoint that is stated but unusable is an error rather than a silent
// downgrade: the user asked for export and would otherwise never learn it is
// not happening.
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
		{"no token", func(o *Options) { o.Token = "" }, tokenEnvName()},
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

// The scrape gate stands alone: no endpoint, no token, no export, and the
// endpoint still answers. This is what makes the local debug loop work with no
// account configured.
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
	// Nothing is exported, so there is no log bridge to attach.
	assert.Nil(t, p.LogWriter())

	body := scrape(t, p.MetricsHandler())

	// Go runtime metrics are the MVP's whole metric surface; assert one of
	// them rather than the exposition being merely non-empty.
	assert.Contains(t, body, "go_memory_used_bytes")

	// The resource identity has to reach the exposition, or a query written
	// locally cannot be the same query run against the remote backend.
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

// An empty identity is omitted rather than sent blank: an empty `instance`
// label is worse than no label at all.
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
