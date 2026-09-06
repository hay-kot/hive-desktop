// Package sourcehttp is the HTTP toolkit a pull connector's API client is
// built over: the failure taxonomy, status and rate-limit mapping,
// conditional requests, and a logging transport. Provider vocabulary and
// source semantics (caching, poll cadence, cooldowns) stay with the caller.
package sourcehttp

import (
	"net/http"
	"time"

	"github.com/hay-kot/appkit/httpclient"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// attrSource names the connector a request belongs to. server.address cannot
// answer that: one host serves several connectors, and under the dev proxy every
// connector shares one address.
const attrSource = "source"

const DefaultTimeout = 30 * time.Second

type Config struct {
	// Name identifies the provider ("github") in log lines and error messages.
	Name    string
	BaseURL string
	Logger  zerolog.Logger
	// Token is resolved per request, so rotation needs no rebuild. nil is
	// anonymous.
	Token   func() string
	Timeout time.Duration
	// Transport is the base the logging transport wraps; nil uses
	// http.DefaultTransport.
	Transport http.RoundTripper
}

// New builds the client a source's API calls go through. Logging is installed
// as a transport rather than middleware so it observes the request that
// reaches the wire, after redirects and regardless of caller middleware.
//
// otelhttp wraps the logging transport rather than the other way round, so the
// span covers everything the log line describes. It is the whole of this
// package's instrumentation: request duration, status and retry counts are
// semconv metrics the library already emits, and hand-writing them here would
// produce the same numbers under names no dashboard knows.
func New(cfg Config, mws ...httpclient.Middleware) *httpclient.Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	httpClient := &http.Client{
		Timeout: timeout,
		Transport: otelhttp.NewTransport(
			NewTransport(cfg.Name, cfg.Logger, cfg.Transport),
			otelhttp.WithSpanNameFormatter(spanName(cfg.Name)),
		),
	}

	chain := make([]httpclient.Middleware, 0, len(mws)+1)
	if cfg.Token != nil {
		chain = append(chain, httpclient.BearerAuth(cfg.Token))
	}
	chain = append(chain, mws...)

	return httpclient.New(httpClient, cfg.BaseURL, chain...)
}

// spanName names a client span after the provider and the method, never the
// path. A source path carries repository and org names, which would make every
// repository its own span name and every trace search over them useless. The
// "http." prefix is what makes the name readable on its own: a bare
// "gitea GET" in a trace list says nothing about which layer produced it.
func spanName(source string) func(string, *http.Request) string {
	return func(_ string, r *http.Request) string { return "http." + source + " " + r.Method }
}
