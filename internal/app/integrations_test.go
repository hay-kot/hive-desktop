package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
)

func integrationsFor(t *testing.T, creds credentials.Store) map[string]Integration {
	t.Helper()
	list, err := newIntegrationsService(creds).List(t.Context())
	require.NoError(t, err)

	byType := make(map[string]Integration, len(list))
	for _, integration := range list {
		byType[integration.Type] = integration
	}
	return byType
}

// The screen is a projection of the registry, not a second list beside it. It
// used to be a hardcoded array, which is a list that drifts silently: a
// connector added to the registry simply never got a card.
func TestIntegrationsListsEveryRegisteredConnector(t *testing.T) {
	t.Parallel()

	list, err := newIntegrationsService(credentials.NewMemoryStore()).List(t.Context())
	require.NoError(t, err)
	require.Len(t, list, len(sources.All()))

	for _, integration := range list {
		descriptor, ok := sources.Lookup(integration.Type)
		require.Truef(t, ok, "listed %q, which is not registered", integration.Type)
		assert.Equal(t, descriptor.Title, integration.Title)
		assert.Equal(t, descriptor.Provider, integration.Provider)
		assert.NotEqual(t, "unknown", integration.Mode, "connector %q", integration.Type)
		assert.NotEqual(t, "unknown", integration.Stability, "connector %q", integration.Type)
	}
}

// Go map iteration is randomized, so an unsorted projection would reshuffle
// the cards on every read — and the screen re-reads on every connection
// change.
func TestIntegrationsAreSortedByType(t *testing.T) {
	t.Parallel()

	list, err := newIntegrationsService(credentials.NewMemoryStore()).List(t.Context())
	require.NoError(t, err)

	for i := 1; i < len(list); i++ {
		assert.Lessf(t, list[i-1].Type, list[i].Type, "entry %d is out of order", i)
	}
}

func TestIntegrationsReportConnectedAccounts(t *testing.T) {
	t.Parallel()

	creds := credentials.NewMemoryStore()
	require.NoError(t, creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: "octocat"}, "tok1"))
	require.NoError(t, creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: "hubot"}, "tok2"))

	github := integrationsFor(t, creds)[ghsource.Descriptor.Type]
	assert.Equal(t, []string{"hubot", "octocat"}, github.Accounts)
	assert.True(t, github.Connected())
}

// A connector with nothing to authenticate as is not "not connected" — the
// webhook listener is local ingress. Its card must not offer a Connect
// action, which is what an empty Provider tells the frontend.
func TestIntegrationWithoutAProviderReportsNoAccounts(t *testing.T) {
	t.Parallel()

	creds := credentials.NewMemoryStore()
	require.NoError(t, creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: "octocat"}, "tok1"))

	for connectorType, integration := range integrationsFor(t, creds) {
		if integration.Provider != "" {
			continue
		}
		assert.Emptyf(t, integration.Accounts, "connector %q has no provider but reports accounts", connectorType)
		assert.Falsef(t, integration.EnvOverride, "connector %q has no provider but reports an override", connectorType)
	}
}

// Accounts is never nil: a nil slice marshals to null, and every frontend read
// of it would need a guard the type does not advertise.
func TestIntegrationAccountsAreNeverNil(t *testing.T) {
	t.Parallel()

	for connectorType, integration := range integrationsFor(t, credentials.NewMemoryStore()) {
		assert.NotNilf(t, integration.Accounts, "connector %q has nil accounts", connectorType)
	}
}

// The environment override authenticates every fetch while naming no account,
// so a listing of stored refs is empty for it. Reporting that as disconnected
// would put "Not connected" beside a feed that is visibly working — which is
// exactly the state CI, the server build and the e2e harness all run in.
func TestIntegrationEnvOverrideCountsAsConnectedWithoutAStoredAccount(t *testing.T) {
	t.Setenv(credentials.EnvOverrideName(ghsource.Provider), "env-token")

	github := integrationsFor(t, credentials.NewMemoryStore())[ghsource.Descriptor.Type]
	assert.Empty(t, github.Accounts)
	assert.True(t, github.EnvOverride)
	assert.True(t, github.Connected())
}

func TestIntegrationWithNoCredentialIsNotConnected(t *testing.T) {
	t.Parallel()

	github := integrationsFor(t, credentials.NewMemoryStore())[ghsource.Descriptor.Type]
	assert.Empty(t, github.Accounts)
	assert.False(t, github.Connected())
}
