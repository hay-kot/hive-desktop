package app

import (
	"context"
	"sort"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
)

// Integration is one connector family as the Integrations screen shows it: what
// the registry declares about it, plus what the credential store currently
// holds for it.
//
// It is a projection of the registry, not a second list. A connector that is
// not registered is not an integration — there is no "coming soon" entry,
// because a card a user cannot act on is a promise the code does not make.
//
// Cards are grouped by credentials provider: a provider that ships several
// connector types (Grafana's metrics and alerts) shows as one card, because
// connecting a stack authenticates both. A connector with no provider (the
// webhook listener is local ingress) is its own card, keyed by its type.
type Integration struct {
	// Key identifies the card: the credentials provider for a credentialed
	// connector ("github", "grafana"), else the connector type
	// ("sources.webhook"). The frontend keys its icon and drawer off it.
	Key       string `json:"key"`
	Title     string `json:"title"`
	Stability string `json:"stability"`
	// Provider is the credentials provider, empty when the connector needs no
	// credential. Empty means the account fields below are meaningless rather
	// than merely empty: a webhook listener is not "not connected".
	Provider string `json:"provider"`
	// Types are the connector node types this card covers, sorted. A family
	// like Grafana lists several; a single-type connector lists one.
	Types []string `json:"types"`
	// Accounts is every connected account for Provider, sorted. Names only —
	// a value never leaves the credential store.
	Accounts []string `json:"accounts"`
	// EnvOverride reports that this provider's environment override is set,
	// which authenticates every fetch without any stored account. Without it
	// a headless or CI run would show "Not connected" beside a working feed.
	EnvOverride bool `json:"envOverride"`
}

// Connected reports whether this integration can currently fetch: either an
// account is stored, or the environment override is supplying one.
func (i Integration) Connected() bool { return len(i.Accounts) > 0 || i.EnvOverride }

// IntegrationsService lists the connector registry with each entry's current
// connection state.
//
// It is deliberately generic — it knows about descriptors and credentials and
// about no particular provider. Acquiring a credential is provider-specific
// and lives on that connector's own service (GitHubService's device flow);
// this is the half that has to work the same for every connector, which is
// what makes adding one a change to internal/app/sources alone.
type IntegrationsService struct{ creds credentials.Store }

func newIntegrationsService(creds credentials.Store) *IntegrationsService {
	return &IntegrationsService{creds: creds}
}

// List returns one card per connector family, sorted by key so the screen's
// order is stable across reads — Go map iteration is randomized, and a card
// list that reshuffles on every poll is unusable.
func (s *IntegrationsService) List(context.Context) ([]Integration, error) {
	// Group descriptors by card key: the provider for a credentialed connector,
	// else its type. One card per provider is what stops a family like Grafana
	// (metrics and alerts) from showing a card per node type.
	groups := map[string][]connector.Descriptor{}
	for _, d := range sources.All() {
		key := d.Provider
		if key == "" {
			key = d.Type
		}
		groups[key] = append(groups[key], d)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]Integration, 0, len(keys))
	for _, key := range keys {
		ds := groups[key]
		sort.Slice(ds, func(i, j int) bool { return ds[i].Type < ds[j].Type })

		types := make([]string, len(ds))
		for i, d := range ds {
			types[i] = d.Type
		}

		integration := Integration{
			Key:       key,
			Title:     groupTitle(ds),
			Stability: leastStable(ds).String(),
			Provider:  ds[0].Provider,
			Types:     types,
			// Never nil: a nil slice marshals to null, and the frontend would
			// have to guard every read of it.
			Accounts: []string{},
		}

		if provider := ds[0].Provider; provider != "" {
			refs, err := credentials.ListProvider(s.creds, provider)
			if err != nil {
				return nil, Wrap(err, KindInternal, "listing connected accounts")
			}
			for _, ref := range refs {
				integration.Accounts = append(integration.Accounts, ref.Account)
			}
			integration.EnvOverride = credentials.HasEnvOverride(provider)
		}

		out = append(out, integration)
	}
	return out, nil
}

// groupTitle is a card's title: one descriptor's own title, or the shared
// prefix of several so "Grafana metrics source" and "Grafana alerts source"
// title a single "Grafana" card.
func groupTitle(ds []connector.Descriptor) string {
	if len(ds) == 1 {
		return ds[0].Title
	}
	prefix := ds[0].Title
	for _, d := range ds[1:] {
		prefix = commonPrefix(prefix, d.Title)
	}
	prefix = strings.TrimRight(prefix, " -–—:")
	if prefix == "" {
		return ds[0].Provider
	}
	return prefix
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// leastStable is the most conservative stability across a provider's
// descriptors: a family with any experimental node type is not yet stable, so
// the card must not read "stable".
func leastStable(ds []connector.Descriptor) connector.Stability {
	least := ds[0].Stability
	for _, d := range ds[1:] {
		if d.Stability < least {
			least = d.Stability
		}
	}
	return least
}
