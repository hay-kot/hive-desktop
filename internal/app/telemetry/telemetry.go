// Package telemetry exports the app's own metrics, logs, and traces over OTLP,
// and serves its metrics for a local scrape (ADR telemetry-is-exported-over-otlp-with-no-collector-and-the-same-instruments-serve-a-local-scrape).
//
// Export and scrape are independent gates over one MeterProvider, so an
// instrument is declared once and both readers collect it. With neither on,
// [New] returns what [Off] returns.
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
	MetricsPath = "/metrics"

	// CredentialProvider derives the token's environment name, so the variable
	// read today is the one a stored credential would use.
	CredentialProvider = "grafanacloud"

	ScopeName = "github.com/hay-kot/hive-desktop"
)

type Options struct {
	// Endpoint is the signal-less OTLP base
	// ("https://otlp-gateway-<zone>.grafana.net/otlp"); per-signal paths are
	// appended. On Grafana Cloud User is the stack's OTLP instance id, which is
	// not the stack id.
	Export   bool
	Endpoint string
	User     string
	Token    string

	Scrape bool

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
	// https rather than the loopback rule development.github.api_base follows:
	// this endpoint is remote by definition, so what is left to enforce is that
	// the credential does not cross the network in the clear.
	if parsed.Scheme != "https" {
		return errors.New("telemetry: endpoint must use https")
	}
	if parsed.Host == "" {
		return errors.New("telemetry: endpoint has no host")
	}
	return nil
}

type Provider struct {
	tracer  trace.Tracer
	handler http.Handler
	logw    io.Writer

	// Run in reverse, so a batch processor flushes before its exporter closes.
	shutdown []func(context.Context) error
}

// Off returns a provider that emits nothing. Its Tracer is a no-op tracer, so a
// span is safe to open without checking whether telemetry is configured.
func Off() *Provider {
	return &Provider{tracer: noop.NewTracerProvider().Tracer(ScopeName)}
}

// New returns [Off] when both gates are off, and an error when export is on but
// misconfigured: a stated endpoint that cannot be used is worth reporting
// rather than silently dropping telemetry over.
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
	fail := func(err error) (*Provider, error) {
		_ = p.Shutdown(ctx)
		return nil, err
	}

	var readers []sdkmetric.Option
	if opts.Scrape {
		// A private registry, never promclient.DefaultRegisterer — the reason
		// PprofHandler builds its own mux.
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
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
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

	if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(mp)); err != nil {
		return fail(fmt.Errorf("telemetry: runtime metrics: %w", err))
	}

	return p, nil
}

func (p *Provider) Enabled() bool                { return len(p.shutdown) > 0 || p.handler != nil }
func (p *Provider) Tracer() trace.Tracer         { return p.tracer }
func (p *Provider) MetricsHandler() http.Handler { return p.handler }
func (p *Provider) LogWriter() io.Writer         { return p.logw }

// LogWriters is LogWriter as a variadic-friendly slice for settings.NewLogger.
func (p *Provider) LogWriters() []io.Writer {
	if p.logw == nil {
		return nil
	}
	return []io.Writer{p.logw}
}

// Shutdown is safe on an Off provider and safe to call more than once.
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

func signalURL(base, signal string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/") + "/v1/" + signal
}

func basicAuth(user, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
}
