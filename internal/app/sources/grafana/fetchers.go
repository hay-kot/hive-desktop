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

// defaultCooldown is how long polling pauses after a rate-limit response that
// carried no explicit reset time.
const defaultCooldown = time.Minute

// Fetchers hands out one fetcher per stack, constructing them on first use. A
// fetcher owns that stack's rate-limit cooldown — sourcehttp classifies a 429
// but does not pause polling, so the provider must — and resolves the stack's
// URL and token freshly on every poll, so connecting, rotating, or
// disconnecting takes effect on the next tick.
//
// Fetchers are never evicted: the set is bounded by the number of connected
// stacks, and a disconnected stack's fetcher holds only a cleared cooldown.
type Fetchers struct {
	stacks    *StackStore
	creds     credentials.Store
	logger    zerolog.Logger
	newClient clientFactory

	mu    sync.Mutex
	byRef map[credentials.Ref]*fetcher
}

// NewFetchers builds the per-stack fetcher registry over the stack store and
// credential store.
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

// For returns the fetcher for one stack, creating it on first use. It never
// fails: a node naming a not-yet-connected account gets a fetcher that resolves
// nothing until the account is connected.
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

// InvalidateAll clears every stack's cooldown. A connect or disconnect calls
// it, so a freshly connected account is not left waiting out a cooldown its
// predecessor incurred.
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

// Query resolves the stack's URL and token, builds a client, and runs one
// PromQL query. A rate-limit response arms the cooldown so the next tick skips
// the fetch until the server's reset time.
func (fx *fetcher) Query(ctx context.Context, dsUID, promql string) (client.QueryResult, error) {
	if until, cooling := fx.inCooldown(); cooling {
		return client.QueryResult{}, fmt.Errorf("grafana %s: %w until %s", fx.ref, sourcehttp.ErrRateLimited, until.Format(time.RFC3339))
	}
	base, err := fx.stacks.URL(fx.ref)
	if err != nil {
		return client.QueryResult{}, fmt.Errorf("grafana %s: reading stack URL: %w", fx.ref, err)
	}
	if base == "" {
		return client.QueryResult{}, fmt.Errorf("grafana %s: stack is not connected", fx.ref)
	}
	token, err := fx.resolve()
	if err != nil {
		return client.QueryResult{}, fmt.Errorf("grafana %s: resolving token: %w", fx.ref, err)
	}
	if token == "" {
		return client.QueryResult{}, fmt.Errorf("grafana %s: %w", fx.ref, sourcehttp.ErrUnauthorized)
	}

	result, err := fx.newClient(base, token).Query(ctx, dsUID, promql)
	if err != nil {
		var rateLimit *sourcehttp.RateLimitError
		if errors.As(err, &rateLimit) {
			fx.enterCooldown(rateLimit.ResetAt)
		}
		return client.QueryResult{}, err
	}
	return result, nil
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
