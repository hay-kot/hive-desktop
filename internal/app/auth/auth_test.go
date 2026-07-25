package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
)

// authAPIServer fakes the two GitHub endpoints liveAuth touches: /user for
// validation and the device flow pair.
func authAPIServer(t *testing.T, validTokens map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		token, ok := parseBearer(r.Header.Get("Authorization"))
		login, valid := validTokens[token]
		if !ok || !valid {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"` + login + `","name":"Test User"}`))
	})
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dev1","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`))
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"granted-token"}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func parseBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return "", false
	}
	return header[len(prefix):], true
}

func newLiveAuthForTest(t *testing.T, creds credentials.Store, validTokens map[string]string, onChange func()) Backend {
	t.Helper()
	server := authAPIServer(t, validTokens)
	client := github.NewClient(github.WithAPIBase(server.URL), github.WithAuthBase(server.URL))
	return NewLiveBackend(client, creds, onChange)
}

// seededCreds is a credential store already holding one GitHub account.
// Credentials are keyed by login now, so "a token is stored" also has to say
// whose it is.
func seededCreds(t *testing.T, login, token string) credentials.Store {
	t.Helper()
	creds := credentials.NewMemoryStore()
	if token != "" {
		require.NoError(t, creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: login}, token))
	}
	return creds
}

// storedToken reads the one stored GitHub token directly, bypassing the
// environment override so a test can tell "stored" from "overridden".
func storedToken(t *testing.T, creds credentials.Store) string {
	t.Helper()
	refs, err := credentials.ListProvider(creds, ghsource.Provider)
	require.NoError(t, err)
	if len(refs) == 0 {
		return ""
	}
	value, err := creds.Get(refs[0])
	require.NoError(t, err)
	return value
}

// deniedDeviceFlowServer fakes a device flow whose token endpoint always
// denies authorization, so pollFlow's failure path runs.
func deniedDeviceFlowServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dev1","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`))
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestLiveStatusNoToken(t *testing.T) {
	t.Parallel()

	auth := newLiveAuthForTest(t, credentials.NewMemoryStore(), nil, nil)
	status := auth.Status(t.Context())
	assert.Equal(t, StateUnauthenticated, status.State)
	assert.Empty(t, status.Message)
}

func TestLiveStatusValidStoredToken(t *testing.T) {
	t.Parallel()

	auth := newLiveAuthForTest(t, seededCreds(t, "hayden", "tok1"), map[string]string{"tok1": "hayden"}, nil)
	status := auth.Status(t.Context())
	assert.Equal(t, StateAuthenticated, status.State)
	assert.Equal(t, "hayden", status.Login)
}

func TestLiveStatusRevokedToken(t *testing.T) {
	t.Parallel()

	auth := newLiveAuthForTest(t, seededCreds(t, "hayden", "revoked"), map[string]string{}, nil)
	status := auth.Status(t.Context())
	assert.Equal(t, StateUnauthenticated, status.State)
	assert.NotEmpty(t, status.Message)
}

func TestLiveAuthSetTokenValidatesAndStores(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()
	var notified sync.WaitGroup
	notified.Add(1)
	auth := newLiveAuthForTest(t, store, map[string]string{"pat-1": "hayden"}, notified.Done)

	status, err := auth.SetToken(t.Context(), " pat-1 ")
	require.NoError(t, err)
	assert.Equal(t, StateAuthenticated, status.State)
	assert.Equal(t, "hayden", status.Login)

	assert.Equal(t, "pat-1", storedToken(t, store))
	notified.Wait()
}

func TestLiveAuthSetTokenRejected(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()
	auth := newLiveAuthForTest(t, store, map[string]string{}, nil)

	_, err := auth.SetToken(t.Context(), "bad-token")
	require.ErrorContains(t, err, "rejected")

	assert.Empty(t, storedToken(t, store))
}

func TestLiveAuthDeviceFlowGrantStoresToken(t *testing.T) {
	t.Parallel()

	store := credentials.NewMemoryStore()
	changed := make(chan struct{}, 1)
	auth := newLiveAuthForTest(t, store, map[string]string{"granted-token": "hayden"}, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	info, err := auth.StartDeviceFlow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "AAAA-BBBB", info.UserCode)
	assert.Equal(t, "https://github.com/login/device", info.VerificationURI)

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("device flow grant did not notify")
	}

	assert.Equal(t, "granted-token", storedToken(t, store))
	assert.Equal(t, StateAuthenticated, auth.Status(t.Context()).State)
}

func TestLiveAuthDeviceFlowDeniedSurfacesMessage(t *testing.T) {
	t.Parallel()

	server := deniedDeviceFlowServer(t)
	client := github.NewClient(github.WithAPIBase(server.URL), github.WithAuthBase(server.URL))
	changed := make(chan struct{}, 1)
	auth := NewLiveBackend(client, credentials.NewMemoryStore(), func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	_, err := auth.StartDeviceFlow(t.Context())
	require.NoError(t, err)

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("device flow denial did not notify")
	}

	status := auth.Status(t.Context())
	assert.Equal(t, StateUnauthenticated, status.State)
	assert.Contains(t, status.Message, "authorization denied")

	// Starting a fresh attempt must clear the stale failure message right
	// away, before the new attempt's own outcome (still ~1s out) arrives.
	_, err = auth.StartDeviceFlow(t.Context())
	require.NoError(t, err)
	assert.Empty(t, auth.Status(t.Context()).Message)
}

func TestLiveAuthSignOutWithEnvOverrideExplains(t *testing.T) {
	t.Setenv(github.EnvToken, "env-token")

	store := seededCreds(t, "hayden", "tok1")
	auth := newLiveAuthForTest(t, store, map[string]string{"tok1": "hayden", "env-token": "hayden"}, nil)
	require.Equal(t, StateAuthenticated, auth.Status(t.Context()).State)

	require.NoError(t, auth.SignOut())

	status := auth.Status(t.Context())
	assert.Equal(t, StateUnauthenticated, status.State)
	assert.Contains(t, status.Message, github.EnvToken)
}

func TestLiveAuthSignOutClearsToken(t *testing.T) {
	t.Parallel()

	store := seededCreds(t, "hayden", "tok1")
	auth := newLiveAuthForTest(t, store, map[string]string{"tok1": "hayden"}, nil)
	require.Equal(t, StateAuthenticated, auth.Status(t.Context()).State)

	require.NoError(t, auth.SignOut())

	assert.Empty(t, storedToken(t, store))
	assert.Equal(t, StateUnauthenticated, auth.Status(t.Context()).State)
}

func TestMockAuthModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, StateAuthenticated, NewMockBackend(true, nil).Status(t.Context()).State)
	assert.Equal(t, StateUnauthenticated, NewMockBackend(false, nil).Status(t.Context()).State)
}

func TestMockAuthDeviceFlowAutoGrants(t *testing.T) {
	t.Parallel()

	changed := make(chan struct{}, 1)
	auth := NewMockBackend(false, func() { changed <- struct{}{} })

	info, err := auth.StartDeviceFlow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "7B4C-Q22F", info.UserCode)

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("mock device flow did not grant")
	}
	assert.Equal(t, StateAuthenticated, auth.Status(t.Context()).State)
	assert.Equal(t, "hayden", auth.Status(t.Context()).Login)
}

func TestMockAuthCancelPreventsLateGrant(t *testing.T) {
	t.Parallel()

	changed := make(chan struct{}, 1)
	auth := NewMockBackend(false, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	_, err := auth.StartDeviceFlow(t.Context())
	require.NoError(t, err)
	auth.CancelDeviceFlow()

	time.Sleep(2 * time.Second) // past mockGrantDelay (1.5s)

	assert.Equal(t, StateUnauthenticated, auth.Status(t.Context()).State)
	select {
	case <-changed:
		t.Fatal("onChange fired after cancel")
	default:
	}
}

func TestMockAuthSetTokenAndSignOut(t *testing.T) {
	t.Parallel()

	auth := NewMockBackend(false, nil)

	_, err := auth.SetToken(t.Context(), "")
	require.Error(t, err)

	status, err := auth.SetToken(t.Context(), "anything")
	require.NoError(t, err)
	assert.Equal(t, StateAuthenticated, status.State)

	require.NoError(t, auth.SignOut())
	assert.Equal(t, StateUnauthenticated, auth.Status(t.Context()).State)
}
