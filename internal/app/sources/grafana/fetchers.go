package grafana

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana/client"
	"github.com/hay-kot/hive-desktop/internal/app/sources/sourcehttp"
)

// defaultCooldown applies when a rate-limit response carried no reset time.
const defaultCooldown = time.Minute

// Fetchers hands out one fetcher per stack, created on first use. A fetcher owns
// that stack's cooldown — sourcehttp classifies a 429 but does not pause
// polling, so the provider must — and resolves URL and token freshly each poll,
// so connect, rotate, and disconnect take effect on the next tick.
type Fetchers struct {
	stacks    *StackStore
	creds     credentials.Store
	logger    zerolog.Logger
	newClient clientFactory

	mu    sync.Mutex
	byRef map[credentials.Ref]*fetcher
}

func NewFetchers(stacks *StackStore, creds credentials.Store, logger zerolog.Logger) *Fetchers {
	return &Fetchers{
		stacks: stacks,
		creds:  creds,
		logger: logger,
		newClient: func(base, token string) *client.Client {
			return client.NewClient(base, token, client.WithLogger(logger))
		},
		byRef: map[credentials.Ref]*fetcher{},
	}
}

// For never fails: a fetcher for a not-yet-connected account resolves nothing until it connects.
func (f *Fetchers) For(ref credentials.Ref) *fetcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fx, ok := f.byRef[ref]; ok {
		return fx
	}
	fx := &fetcher{
		ref:       ref,
		stacks:    f.stacks,
		resolve:   credentials.Bind(f.creds, ref),
		newClient: f.newClient,
	}
	f.byRef[ref] = fx
	return fx
}

// InvalidateAll clears every stack's cooldown so a freshly connected account is
// not left waiting out a cooldown its predecessor incurred.
func (f *Fetchers) InvalidateAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fx := range f.byRef {
		fx.clearCooldown()
	}
}

// fetcher runs one stack's queries, gated by that stack's cooldown.
type fetcher struct {
	ref       credentials.Ref
	stacks    *StackStore
	resolve   credentials.Resolver
	newClient clientFactory

	mu            sync.Mutex
	cooldownUntil time.Time
}

func (fx *fetcher) Query(ctx context.Context, dsUID, promql string) (client.QueryResult, error) {
	c, err := fx.prepare()
	if err != nil {
		return client.QueryResult{}, err
	}
	result, err := c.Query(ctx, dsUID, promql)
	if err != nil {
		fx.noteError(err)
		return client.QueryResult{}, err
	}
	return result, nil
}

func (fx *fetcher) Alerts(ctx context.Context) ([]client.Alert, error) {
	c, err := fx.prepare()
	if err != nil {
		return nil, err
	}
	alerts, err := c.Alerts(ctx)
	if err != nil {
		fx.noteError(err)
		return nil, err
	}
	return alerts, nil
}

// prepare returns a client, or an error when the stack is in cooldown, not
// connected, or has no resolvable token — the states a poll must not reach the
// network in.
func (fx *fetcher) prepare() (*client.Client, error) {
	if until, cooling := fx.inCooldown(); cooling {
		return nil, fmt.Errorf("grafana %s: %w until %s", fx.ref, sourcehttp.ErrRateLimited, until.Format(time.RFC3339))
	}
	base, err := fx.stacks.URL(fx.ref)
	if err != nil {
		return nil, fmt.Errorf("grafana %s: reading stack URL: %w", fx.ref, err)
	}
	if base == "" {
		return nil, fmt.Errorf("grafana %s: stack is not connected", fx.ref)
	}
	token, err := fx.resolve()
	if err != nil {
		return nil, fmt.Errorf("grafana %s: resolving token: %w", fx.ref, err)
	}
	if token == "" {
		return nil, fmt.Errorf("grafana %s: %w", fx.ref, sourcehttp.ErrUnauthorized)
	}
	return fx.newClient(base, token), nil
}

// noteError arms the cooldown when the failure was a rate limit, so the next
// tick waits out the server's reset instead of hammering it.
func (fx *fetcher) noteError(err error) {
	var rateLimit *sourcehttp.RateLimitError
	if errors.As(err, &rateLimit) {
		fx.enterCooldown(rateLimit.ResetAt)
	}
}

func (fx *fetcher) inCooldown() (time.Time, bool) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	if fx.cooldownUntil.IsZero() || time.Now().After(fx.cooldownUntil) {
		return time.Time{}, false
	}
	return fx.cooldownUntil, true
}

func (fx *fetcher) enterCooldown(until time.Time) {
	if until.IsZero() {
		until = time.Now().Add(defaultCooldown)
	}
	fx.mu.Lock()
	fx.cooldownUntil = until
	fx.mu.Unlock()
}

func (fx *fetcher) clearCooldown() {
	fx.mu.Lock()
	fx.cooldownUntil = time.Time{}
	fx.mu.Unlock()
}
