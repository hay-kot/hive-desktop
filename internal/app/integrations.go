package app

import (
	"context"
	"sort"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources"
)

// Integration is one connector as the Integrations screen shows it: what the
// registry declares about it, plus what the credential store currently holds
// for it.
//
// It is a projection of the registry, not a second list. A connector that is
// not registered is not an integration — there is no "coming soon" entry,
// because a card a user cannot act on is a promise the code does not make.
type Integration struct {
	// Type is the connector's namespaced type, "sources.github". It is the
	// identity the frontend keys its icon and drawer off.
	Type      string `json:"type"`
	Title     string `json:"title"`
	Stability string `json:"stability"`
	// Mode is "pull" or "push".
	Mode string `json:"mode"`
	// Provider is the credentials provider, empty when the connector needs no
	// credential. Empty means the account fields below are meaningless rather
	// than merely empty: a webhook listener is not "not connected".
	Provider string `json:"provider"`
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

// List returns every registered connector, sorted by type so the screen's
// order is stable across reads — Go map iteration is randomized, and a card
// list that reshuffles on every poll is unusable.
func (s *IntegrationsService) List(context.Context) ([]Integration, error) {
	descriptors := sources.All()

	types := make([]string, 0, len(descriptors))
	for connectorType := range descriptors {
		types = append(types, connectorType)
	}
	sort.Strings(types)

	out := make([]Integration, 0, len(types))
	for _, connectorType := range types {
		d := descriptors[connectorType]
		integration := Integration{
			Type:      d.Type,
			Title:     d.Title,
			Stability: d.Stability.String(),
			Mode:      d.Mode.String(),
			Provider:  d.Provider,
			// Never nil: a nil slice marshals to null, and the frontend would
			// have to guard every read of it.
			Accounts: []string{},
		}

		if d.Provider != "" {
			refs, err := credentials.ListProvider(s.creds, d.Provider)
			if err != nil {
				return nil, Wrap(err, KindInternal, "listing connected accounts")
			}
			for _, ref := range refs {
				integration.Accounts = append(integration.Accounts, ref.Account)
			}
			integration.EnvOverride = credentials.HasEnvOverride(d.Provider)
		}

		out = append(out, integration)
	}
	return out, nil
}
