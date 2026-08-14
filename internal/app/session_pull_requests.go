package app

import (
	"context"
	"sync"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/ghclient"
)

// sessionPRCacheTTL bounds how stale a session's pull-request badge may be.
// The status bar polls its git half far more often than this: git is local
// subprocesses, this is a network round trip against a shared rate limit.
const sessionPRCacheTTL = 5 * time.Minute

// sessionPullRequests answers "what is this branch's pull request" for the
// session status bar, over the app's own GitHub client rather than the `gh`
// CLI. That is what lets it keep "no pull request" apart from "the lookup
// failed" — a distinction the CLI path loses, because it caches an empty
// result on error and shows a blank badge for the whole TTL.
type sessionPullRequests struct {
	client *ghclient.Client
	creds  credentials.Store

	mu     sync.Mutex
	cached map[dispatch.SessionPullRequestKey]cachedPullRequest
	// now is the clock, injected so a test does not sleep out a TTL.
	now func() time.Time
}

type cachedPullRequest struct {
	view   dispatch.SessionPullRequest
	readAt time.Time
}

func newSessionPullRequests(client *ghclient.Client, creds credentials.Store) *sessionPullRequests {
	return &sessionPullRequests{
		client: client,
		creds:  creds,
		cached: map[dispatch.SessionPullRequestKey]cachedPullRequest{},
		now:    time.Now,
	}
}

// Lookup resolves one branch's pull request, answering from cache while the
// entry is fresh. refresh discards the cached entry first, which is what a
// user clicking the badge asks for.
func (p *sessionPullRequests) Lookup(ctx context.Context, key dispatch.SessionPullRequestKey, refresh bool) (dispatch.SessionPullRequest, error) {
	if key.Owner == "" || key.Repo == "" || key.Branch == "" {
		// Not a GitHub remote, or a checkout whose branch did not resolve.
		// Neither is a failure the bar should report as one.
		return dispatch.SessionPullRequest{Status: dispatch.PullRequestStatusUnsupported}, nil
	}

	if !refresh {
		if view, ok := p.fresh(key); ok {
			return view, nil
		}
	}

	view, err := p.fetch(ctx, key)
	if err != nil {
		return dispatch.SessionPullRequest{}, err
	}
	p.mu.Lock()
	p.cached[key] = cachedPullRequest{view: view, readAt: p.now()}
	p.mu.Unlock()
	return view, nil
}

func (p *sessionPullRequests) fresh(key dispatch.SessionPullRequestKey) (dispatch.SessionPullRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cached[key]
	if !ok || p.now().Sub(entry.readAt) > sessionPRCacheTTL {
		return dispatch.SessionPullRequest{}, false
	}
	return entry.view, true
}

func (p *sessionPullRequests) fetch(ctx context.Context, key dispatch.SessionPullRequestKey) (dispatch.SessionPullRequest, error) {
	if p.client == nil || p.creds == nil {
		return dispatch.SessionPullRequest{Status: dispatch.PullRequestStatusDisconnected}, nil
	}

	tokens, err := p.tokens()
	if err != nil {
		return dispatch.SessionPullRequest{}, Wrap(err, KindInternal, "reading GitHub credentials")
	}
	if len(tokens) == 0 {
		return dispatch.SessionPullRequest{Status: dispatch.PullRequestStatusDisconnected}, nil
	}

	ref := ghclient.BranchRef{Owner: key.Owner, Repo: key.Repo, Branch: key.Branch}
	// Several connected accounts are the reason this loops: a repository one
	// account cannot see resolves to a null alias, not an error, so the only
	// way to know another account can see it is to ask.
	var lastErr error
	for _, token := range tokens {
		results, err := p.client.WithTokenCopy(token).PullRequestsByBranch(ctx, []ghclient.BranchRef{ref})
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) == 0 || !results[0].Found {
			continue
		}
		return viewOfPullRequest(results[0]), nil
	}
	if lastErr != nil {
		return dispatch.SessionPullRequest{}, Wrap(lastErr, KindInternal, "reading the pull request for %s", key.Branch)
	}
	return dispatch.SessionPullRequest{Status: dispatch.PullRequestStatusNone}, nil
}

// tokens lists every token that could see the repository. The environment
// override is provider-wide and names no account, so a headless run stores no
// ref at all and would otherwise list nothing; the ref below exists only to
// give credentials.Resolve something well-formed to answer the override with.
func (p *sessionPullRequests) tokens() ([]string, error) {
	refs, err := credentials.ListProvider(p.creds, ghsource.Provider)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 && credentials.HasEnvOverride(ghsource.Provider) {
		refs = []credentials.Ref{{Provider: ghsource.Provider, Account: "env"}}
	}
	tokens := make([]string, 0, len(refs))
	for _, ref := range refs {
		token, err := credentials.Resolve(p.creds, ref)
		if err != nil || token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func viewOfPullRequest(pr ghclient.PullRequest) dispatch.SessionPullRequest {
	return dispatch.SessionPullRequest{
		Status:         dispatch.PullRequestStatusFound,
		Number:         pr.Number,
		Title:          pr.Title,
		State:          pr.State,
		IsDraft:        pr.IsDraft,
		URL:            pr.URL,
		ReviewDecision: pr.ReviewDecision,
		Checks:         string(pr.Checks),
	}
}
