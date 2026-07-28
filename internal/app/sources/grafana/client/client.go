// Package client is the desktop's Grafana HTTP client: token validation via
// GET /api/org/ and a PromQL query through the datasource proxy. It owns no
// source semantics — cooling off after a rate limit, resolving credentials, and
// caching all live in the connector — and no persistence: a client is built per
// tick from a base URL and a freshly resolved token.
//
// HTTP plumbing — the failure taxonomy, status and rate-limit mapping, request
// logging — comes from sources/sourcehttp.
package client

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/hay-kot/appkit/httpclient"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/sources/sourcehttp"
)

const sourceName = "grafana"

// Org is the organization a token authenticates against, from GET /api/org/.
// The id is half of a stack's account identity (host + org id), because one
// stack can host several orgs and a token belongs to exactly one.
type Org struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// QueryResult is the data object of a Prometheus query response, carried
// verbatim so a function node can inspect resultType and result without the
// connector modelling Prometheus' value shapes.
type QueryResult struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

// Alert is one firing Grafana-managed alert instance from the Alertmanager v2
// API. Fingerprint is the stable per-instance identity the connector keys an
// inbox item on; a still-firing alert re-reports the same fingerprint.
type Alert struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	Status      struct {
		State string `json:"state"`
	} `json:"status"`
}

// Client talks to one Grafana stack as one token.
type Client struct {
	api  *httpclient.Client
	errs sourcehttp.Errors
}

type options struct {
	logger zerolog.Logger
}

// Option configures a Client.
type Option func(*options)

// WithLogger enables request logging at debug level.
func WithLogger(logger zerolog.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// NewClient builds a client for one stack, authenticated with one token. It is
// constructed per tick from a freshly resolved token rather than held across
// ticks, so a rotated or disconnected credential is never fetched with a stale
// copy.
func NewClient(base, token string, opts ...Option) *Client {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{
		api: sourcehttp.New(sourcehttp.Config{
			Name:    sourceName,
			BaseURL: base,
			Logger:  o.logger,
			Token:   func() string { return token },
		}, httpclient.Header("Accept", "application/json")),
		errs: sourcehttp.Errors{Name: sourceName},
	}
}

// ValidateToken reads the token's current organization. It doubles as token
// validation: ErrUnauthorized means the token is missing, revoked, or scoped to
// no organization.
func (c *Client) ValidateToken(ctx context.Context) (Org, error) {
	resp, err := c.api.Get(ctx, "/api/org/")
	if err != nil {
		return Org{}, c.errs.Unreachable(err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	if err := c.errs.Status(resp); err != nil {
		return Org{}, err
	}
	var org Org
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return Org{}, c.errs.Errorf("decode org: %w", err)
	}
	return org, nil
}

// Query runs a PromQL instant query through the datasource proxy against the
// datasource identified by uid. The proxy forwards to the datasource's
// Prometheus-compatible /api/v1/query, so the response is the standard
// Prometheus envelope; a non-success status in the body is an error even when
// the HTTP status was 200.
func (c *Client) Query(ctx context.Context, dsUID, promql string) (QueryResult, error) {
	path := "/api/datasources/proxy/uid/" + url.PathEscape(dsUID) + "/api/v1/query"
	form := url.Values{"query": {promql}}.Encode()

	resp, err := c.api.Post(ctx, path, strings.NewReader(form),
		httpclient.Header("Content-Type", "application/x-www-form-urlencoded"))
	if err != nil {
		return QueryResult{}, c.errs.Unreachable(err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	if err := c.errs.Status(resp); err != nil {
		return QueryResult{}, err
	}

	var envelope struct {
		Status    string      `json:"status"`
		Data      QueryResult `json:"data"`
		ErrorType string      `json:"errorType"`
		Error     string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return QueryResult{}, c.errs.Errorf("decode query response: %w", err)
	}
	if envelope.Status != "success" {
		msg := envelope.Error
		if msg == "" {
			msg = envelope.Status
		}
		return QueryResult{}, c.errs.Errorf("query failed: %s", msg)
	}
	return envelope.Data, nil
}

// Alerts lists the stack's currently firing Grafana-managed alerts through the
// Alertmanager v2 API. The response is the complete active set, which is what
// lets the connector treat an alert's absence as authoritatively resolved.
func (c *Client) Alerts(ctx context.Context) ([]Alert, error) {
	resp, err := c.api.Get(ctx, "/api/alertmanager/grafana/api/v2/alerts")
	if err != nil {
		return nil, c.errs.Unreachable(err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body close

	if err := c.errs.Status(resp); err != nil {
		return nil, err
	}
	var alerts []Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, c.errs.Errorf("decode alerts: %w", err)
	}
	return alerts, nil
}
