// Package telemetry exports the desktop app's own metrics, logs, and traces
// over OTLP, and serves its metrics for a local scrape.
//
// The two are independent gates over one MeterProvider: export pushes to a
// remote OTLP endpoint, scrape mounts [MetricsPath] on the loopback HTTP
// server. An instrument is declared once and both readers collect it, so a
// PromQL expression written against the local endpoint transfers to the
// remote backend unchanged.
//
// With neither gate on, [New] returns the same no-op object [Off] does: no
// provider is constructed, no exporter goroutine runs, and no call site needs
// a nil check.
package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	// MetricsPath is where [Provider.MetricsHandler] mounts on the shared
	// loopback HTTP server, following pprof's precedent (ADR pprof-debug-endpoint).
	MetricsPath = "/metrics"

	// CredentialProvider names the credential provider whose environment
	// override carries the OTLP token, so the variable this reads today
	// (HIVE_GRAFANACLOUD_TOKEN) is the one a stored credential would use.
	CredentialProvider = "grafanacloud"

	// ScopeName identifies this app's own instrumentation, as opposed to a
	// library's, on every signal it emits.
	ScopeName = "github.com/hay-kot/hive-desktop"
)

// Options configures the provider. Export and Scrape are independent: either,
// both, or neither.
type Options struct {
	// Export pushes OTLP/HTTP to Endpoint, which is the signal-less base
	// ("https://otlp-gateway-<zone>.grafana.net/otlp"); the per-signal paths
	// are appended.
	Export   bool
	Endpoint string
	// User and Token are the endpoint's basic-auth pair. On Grafana Cloud User
	// is the stack's OTLP instance id, which is not the stack id — it is the
	// one printed on the stack's OpenTelemetry tile.
	User  string
	Token string

	// Scrape serves MetricsPath from a Prometheus reader on the same
	// MeterProvider Export reads from.
	Scrape bool

	// Version, Environment and Instance become the resource attributes every
	// signal carries. See resource.go for why these three.
	Version     string
	Environment string
	Instance    string
}

func (o Options) validate() error {
	if !o.Export {
		return nil
	}
	if strings.TrimSpace(o.Endpoint) == "" {
		return errors.New("telemetry: endpoint is required to export")
	}
	if strings.TrimSpace(o.User) == "" {
		return errors.New("telemetry: user is required to export")
	}
	if strings.TrimSpace(o.Token) == "" {
		return fmt.Errorf("telemetry: no token; set %s", tokenEnvName())
	}
	parsed, err := url.Parse(o.Endpoint)
	if err != nil {
		return fmt.Errorf("telemetry: endpoint is not a URL: %w", err)
	}
	// https only. Unlike development.github.api_base, which is pinned to
	// loopback so a persisted setting cannot aim the app at a remote host,
	// this endpoint is remote by definition — so the check that is left to
	// make is that the credential does not cross the network in the clear.
	if parsed.Scheme != "https" {
		return errors.New("telemetry: endpoint must use https")
	}
	if parsed.Host == "" {
		return errors.New("telemetry: endpoint has no host")
	}
	return nil
}

// Provider owns the SDK objects and hands out the narrow surfaces the rest of
// the app uses. Exactly one is constructed per process.
type Provider struct {
	tracer  trace.Tracer
	handler http.Handler
	logw    io.Writer

	// shutdown is run in reverse order, so a batch processor flushes before
	// the exporter it writes through is closed.
	shutdown []func(context.Context) error
}

// Off returns a provider that emits nothing. Its Tracer is a no-op tracer and
// its handler and writer are nil, so a caller holds a usable *Provider whether
// or not telemetry is configured.
func Off() *Provider {
	return &Provider{tracer: noop.NewTracerProvider().Tracer(ScopeName)}
}

// New builds the provider described by opts. It returns [Off] when both gates
// are off, and an error when export is on but misconfigured — a stated
// endpoint that cannot be used is a mistake worth reporting, not one to
// silently drop telemetry over.
func New(ctx context.Context, opts Options) (*Provider, error) {
	if !opts.Export && !opts.Scrape {
		return Off(), nil
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}

	res, err := newResource(opts)
	if err != nil {
		return nil, err
	}

	p := Off()
	// Anything already built is torn down before the error leaves, so a
	// half-constructed provider never outlives the failure.
	fail := func(err error) (*Provider, error) {
		_ = p.Shutdown(ctx)
		return nil, err
	}

	var readers []sdkmetric.Option
	if opts.Scrape {
		// A private registry, never promclient.DefaultRegisterer: the same
		// reason PprofHandler builds its own mux instead of writing to
		// http.DefaultServeMux.
		reg := promclient.NewRegistry()
		promReader, err := otelprom.New(otelprom.WithRegisterer(reg))
		if err != nil {
			return fail(fmt.Errorf("telemetry: prometheus reader: %w", err))
		}
		readers = append(readers, sdkmetric.WithReader(promReader))
		p.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	}

	if opts.Export {
		headers := map[string]string{"Authorization": basicAuth(opts.User, opts.Token)}

		metricExp, err := otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpointURL(signalURL(opts.Endpoint, "metrics")),
			otlpmetrichttp.WithHeaders(headers),
			otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression),
		)
		if err != nil {
			return fail(fmt.Errorf("telemetry: metric exporter: %w", err))
		}
		readers = append(readers, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))

		traceExp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(signalURL(opts.Endpoint, "traces")),
			otlptracehttp.WithHeaders(headers),
			otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
		)
		if err != nil {
			return fail(fmt.Errorf("telemetry: trace exporter: %w", err))
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
		)
		p.tracer = tp.Tracer(ScopeName)
		p.shutdown = append(p.shutdown, tp.Shutdown)

		logExp, err := otlploghttp.New(ctx,
			otlploghttp.WithEndpointURL(signalURL(opts.Endpoint, "logs")),
			otlploghttp.WithHeaders(headers),
			otlploghttp.WithCompression(otlploghttp.GzipCompression),
		)
		if err != nil {
			return fail(fmt.Errorf("telemetry: log exporter: %w", err))
		}
		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
		p.logw = newLogWriter(ctx, lp.Logger(ScopeName))
		p.shutdown = append(p.shutdown, lp.Shutdown)
	}

	mp := sdkmetric.NewMeterProvider(append(readers, sdkmetric.WithResource(res))...)
	p.shutdown = append(p.shutdown, mp.Shutdown)

	// Go runtime metrics are the MVP's whole metric surface: they need no
	// instrumentation in the app and they are what "is the process healthy"
	// is answered from.
	if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(mp)); err != nil {
		return fail(fmt.Errorf("telemetry: runtime metrics: %w", err))
	}

	return p, nil
}

// Enabled reports whether anything is emitted.
func (p *Provider) Enabled() bool { return len(p.shutdown) > 0 || p.handler != nil }

// Tracer returns the app's tracer, which is a no-op tracer when export is off.
// Spans are therefore safe to open unconditionally.
func (p *Provider) Tracer() trace.Tracer { return p.tracer }

// MetricsHandler serves the Prometheus exposition format, or nil when the
// scrape gate is off.
func (p *Provider) MetricsHandler() http.Handler { return p.handler }

// LogWriter is a zerolog writer arm that forwards each event as an OTLP log
// record, or nil when export is off. It is a writer and not a zerolog.Hook
// because a Hook sees only the level and the message, while a writer receives
// the encoded event with its fields intact.
func (p *Provider) LogWriter() io.Writer { return p.logw }

// LogWriters returns LogWriter as a variadic-friendly slice, empty when there
// is nothing to forward to.
func (p *Provider) LogWriters() []io.Writer {
	if p.logw == nil {
		return nil
	}
	return []io.Writer{p.logw}
}

// Shutdown flushes and closes everything New built. It is safe on a provider
// from Off, and safe to call more than once.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range slices.Backward(p.shutdown) {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	p.shutdown = nil
	return errors.Join(errs...)
}

// signalURL appends OTLP's per-signal path to the gateway base.
func signalURL(base, signal string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/") + "/v1/" + signal
}

func basicAuth(user, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
}
