// Package auth implements GitHub authentication for the Hive desktop app:
// the live (device flow + PAT) and mock backends and the wire types the
// onboarding UI consumes. The Wails service over them is
// wailsui.AuthService; the GitHub client itself is internal/hivecore/github.
package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
)

// Auth states on the wire. Strings, not enums: the frontend narrows them.
const (
	StateUnauthenticated = "unauthenticated"
	StateAuthenticated   = "authenticated"
)

// EnvGitHubClientID overrides the OAuth app client ID used by the device
// flow, e.g. to test against a different OAuth app registration.
const EnvGitHubClientID = "HIVE_GITHUB_CLIENT_ID"

// defaultClientID is the registered Hive Desktop OAuth app. Client IDs are
// public; the device flow uses no client secret.
const defaultClientID = "Ov23likA3JPBPkYbMGu4"

// deviceFlowScopes: repo covers PR/issue search on private repos;
// notifications covers the inbox feed.
var deviceFlowScopes = []string{"repo", "notifications"}

// Status is the authentication state shown to the frontend.
type Status struct {
	State     string `json:"state"` // StateUnauthenticated | StateAuthenticated
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	// Message carries a user-facing problem description (invalid stored
	// token, device flow failure) without changing the state machine.
	Message string `json:"message"`
}

// DeviceFlowInfo is the pending device authorization the onboarding screen
// renders: the user opens VerificationURI and enters UserCode.
type DeviceFlowInfo struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
}

// Backend implements authentication for Service. liveAuth talks to
// GitHub; mockAuth drives deterministic onboarding for tests and e2e.
type Backend interface {
	Status(ctx context.Context) Status
	StartDeviceFlow(ctx context.Context) (DeviceFlowInfo, error)
	CancelDeviceFlow()
	SetToken(ctx context.Context, token string) (Status, error)
	SignOut() error
}

// ── Live backend ─────────────────────────────────────────────────────────────

type liveAuth struct {
	client *github.Client
	// creds is the credential store. A token is keyed by the login it belongs
	// to, so storing one needs the user lookup that validation already
	// performs.
	creds    credentials.Store
	clientID string
	// onChange is called after every state transition (device flow grant or
	// failure, token set, sign-out). main.go wires it to the auth:updated
	// event emit.
	onChange func()

	mu         sync.Mutex
	flowCancel context.CancelFunc
	cached     *Status
}

func NewLiveBackend(client *github.Client, creds credentials.Store, onChange func()) Backend {
	clientID := os.Getenv(EnvGitHubClientID)
	if clientID == "" {
		clientID = defaultClientID
	}
	return &liveAuth{
		client:   client,
		creds:    creds,
		clientID: clientID,
		onChange: onChange,
	}
}

// connectedRefs is every stored GitHub credential. Several are representable
// now that credentials are keyed by account; this backend still speaks of one
// signed-in user, which is the shape the Integrations screen replaces.
func (a *liveAuth) connectedRefs() ([]credentials.Ref, error) {
	return credentials.ListProvider(a.creds, ghsource.Provider)
}

// token is the token to validate against, preferring the environment
// override so a headless run needs no stored credential at all.
func (a *liveAuth) token() (string, error) {
	if value := os.Getenv(credentials.EnvOverrideName(ghsource.Provider)); value != "" {
		return value, nil
	}
	refs, err := a.connectedRefs()
	if err != nil || len(refs) == 0 {
		return "", err
	}
	return credentials.Resolve(a.creds, refs[0])
}

func (a *liveAuth) Status(ctx context.Context) Status {
	a.mu.Lock()
	if a.cached != nil {
		defer a.mu.Unlock()
		return *a.cached
	}
	a.mu.Unlock()

	token, err := a.token()
	if err != nil {
		return Status{State: StateUnauthenticated, Message: err.Error()}
	}
	if token == "" {
		return Status{State: StateUnauthenticated}
	}

	user, err := a.client.WithTokenCopy(token).User(ctx)
	switch {
	case errors.Is(err, github.ErrUnauthorized):
		return Status{State: StateUnauthenticated, Message: "Stored GitHub token is no longer valid."}
	case err != nil:
		// Unreachable/rate limited with a stored token: optimistically
		// authenticated so the feed shell (with its unreachable state) shows
		// instead of onboarding.
		return Status{State: StateAuthenticated, Message: err.Error()}
	}

	status := authenticatedStatus(user)
	a.setCached(status)
	return status
}

func (a *liveAuth) StartDeviceFlow(ctx context.Context) (DeviceFlowInfo, error) {
	auth, err := a.client.StartDeviceFlow(ctx, a.clientID, deviceFlowScopes)
	if err != nil {
		return DeviceFlowInfo{}, fmt.Errorf("start device flow: %w", err)
	}

	// Detached from the caller's context, not rooted at Background: the poll
	// must outlive the StartDeviceFlow RPC that began it, but it should still
	// carry that call's values.
	pollCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(auth.ExpiresIn)*time.Second)
	a.mu.Lock()
	if a.flowCancel != nil {
		a.flowCancel()
	}
	a.flowCancel = cancel
	// A fresh attempt supersedes any earlier failure message.
	a.cached = nil
	a.mu.Unlock()

	go a.pollFlow(pollCtx, auth)

	return DeviceFlowInfo{UserCode: auth.UserCode, VerificationURI: auth.VerificationURI}, nil
}

func (a *liveAuth) pollFlow(ctx context.Context, auth github.DeviceAuth) {
	token, err := a.client.PollDeviceFlow(ctx, a.clientID, auth)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.setCached(Status{State: StateUnauthenticated, Message: flowFailureMessage(err)})
		a.notify()
		return
	}

	// Not the poll context: its device-code deadline may be about to fire,
	// and validating a just-granted token must not race it.
	a.adoptToken(context.WithoutCancel(ctx), token)
}

// flowFailureMessage maps device-flow failures onto user-facing text. The
// local poll deadline (ExpiresIn) usually fires before GitHub ever reports
// expired_token, so DeadlineExceeded means the code expired unused.
func flowFailureMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "The sign-in code expired before authorization. Start again to get a fresh code."
	}
	return err.Error()
}

func (a *liveAuth) SetToken(ctx context.Context, token string) (Status, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Status{}, fmt.Errorf("token is empty")
	}

	user, err := a.client.WithTokenCopy(token).User(ctx)
	if errors.Is(err, github.ErrUnauthorized) {
		return Status{}, fmt.Errorf("GitHub rejected the token")
	}
	if err != nil {
		return Status{}, fmt.Errorf("validate token: %w", err)
	}

	if err := a.creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: user.Login}, token); err != nil {
		return Status{}, err
	}
	status := authenticatedStatus(user)
	a.setCached(status)
	a.notify()
	return status, nil
}

// adoptToken validates and stores a token granted by the device flow, then
// notifies. Errors surface through the cached status: the poll goroutine has
// no caller to return them to.
func (a *liveAuth) adoptToken(ctx context.Context, token string) {
	user, err := a.client.WithTokenCopy(token).User(ctx)
	if err != nil {
		a.setCached(Status{State: StateUnauthenticated, Message: fmt.Sprintf("validate granted token: %v", err)})
		a.notify()
		return
	}
	if err := a.creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: user.Login}, token); err != nil {
		a.setCached(Status{State: StateUnauthenticated, Message: err.Error()})
		a.notify()
		return
	}
	a.setCached(authenticatedStatus(user))
	a.notify()
}

func (a *liveAuth) CancelDeviceFlow() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.flowCancel != nil {
		a.flowCancel()
		a.flowCancel = nil
	}
}

func (a *liveAuth) SignOut() error {
	a.CancelDeviceFlow()
	// Every connected account, not just one: sign-out means "this app holds
	// no GitHub credentials", and leaving one behind would keep the feed
	// fetching as an account the user believes they removed.
	refs, err := a.connectedRefs()
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := a.creds.Delete(ref); err != nil {
			return err
		}
	}
	status := Status{State: StateUnauthenticated}
	// The env override outranks the keychain, so deleting the keychain entry
	// cannot revoke access; say so instead of silently bouncing back to
	// authenticated on the next Status read.
	if envName := credentials.EnvOverrideName(ghsource.Provider); os.Getenv(envName) != "" {
		status.Message = "Signed out, but the " + envName + " environment override is still set and keeps this session authenticated."
	}
	a.setCached(status)
	a.notify()
	return nil
}

// setCached pins the status Status() returns. Failure statuses are cached
// too: auth:updated carries no payload, so the Message must survive until
// the frontend's follow-up Status() read.
func (a *liveAuth) setCached(status Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cached = &status
}

func (a *liveAuth) notify() {
	if a.onChange != nil {
		a.onChange()
	}
}

func authenticatedStatus(user github.User) Status {
	return Status{
		State:     StateAuthenticated,
		Login:     user.Login,
		Name:      user.Name,
		AvatarURL: user.AvatarURL,
	}
}

// ── Mock backend ─────────────────────────────────────────────────────────────

// mockAuth drives the onboarding flow deterministically and offline. In
// "feed" mock mode it starts authenticated; in "onboarding" mode it starts
// signed out and grants the fake device flow after a short delay.
type mockAuth struct {
	// creds is written on grant and cleared on sign-out, exactly as the live
	// backend does. A mock that only flipped a status flag would leave the
	// credential store empty, and everything downstream that resolves an
	// account — a source node's credential, seeding a workspace — would fail
	// in mock mode only.
	creds    credentials.Store
	onChange func()

	mu     sync.Mutex
	status Status
	timer  *time.Timer
	// flowSeq invalidates a pending grant whose timer already fired but is
	// blocked on mu when CancelDeviceFlow runs: timer.Stop() returns false
	// then, so the callback must re-check it is still the current flow.
	flowSeq int
}

const mockGrantDelay = 1500 * time.Millisecond

func NewMockBackend(authenticated bool, creds credentials.Store, onChange func()) Backend {
	a := &mockAuth{creds: creds, status: Status{State: StateUnauthenticated}, onChange: onChange}
	if authenticated {
		a.status = mockAuthenticatedStatus()
		a.connect()
	}
	return a
}

// mockLogin is the account every mock mode is signed in as.
const mockLogin = "hayden"

func mockAuthenticatedStatus() Status {
	return Status{State: StateAuthenticated, Login: mockLogin, Name: "Hayden"}
}

// connect stores the fake credential. Errors are ignored: the store is always
// the in-memory one in mock modes, where a write cannot fail.
func (a *mockAuth) connect() {
	if a.creds != nil {
		_ = a.creds.Set(credentials.Ref{Provider: ghsource.Provider, Account: mockLogin}, "mock-token")
	}
}

func (a *mockAuth) disconnect() {
	if a.creds != nil {
		_ = a.creds.Delete(credentials.Ref{Provider: ghsource.Provider, Account: mockLogin})
	}
}

func (a *mockAuth) Status(context.Context) Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *mockAuth) StartDeviceFlow(context.Context) (DeviceFlowInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
	}
	a.flowSeq++
	seq := a.flowSeq
	a.timer = time.AfterFunc(mockGrantDelay, func() {
		a.mu.Lock()
		if a.flowSeq != seq {
			a.mu.Unlock()
			return
		}
		a.status = mockAuthenticatedStatus()
		a.connect()
		a.mu.Unlock()
		if a.onChange != nil {
			a.onChange()
		}
	})
	return DeviceFlowInfo{UserCode: "7B4C-Q22F", VerificationURI: "https://github.com/login/device"}, nil
}

func (a *mockAuth) CancelDeviceFlow() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flowSeq++
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
}

func (a *mockAuth) SetToken(_ context.Context, token string) (Status, error) {
	if strings.TrimSpace(token) == "" {
		return Status{}, fmt.Errorf("token is empty")
	}
	a.mu.Lock()
	a.flowSeq++ // a pending device grant must not re-fire over an explicit token
	a.status = mockAuthenticatedStatus()
	a.connect()
	a.mu.Unlock()
	if a.onChange != nil {
		a.onChange()
	}
	return mockAuthenticatedStatus(), nil
}

func (a *mockAuth) SignOut() error {
	a.CancelDeviceFlow()
	a.mu.Lock()
	a.status = Status{State: StateUnauthenticated}
	a.disconnect()
	a.mu.Unlock()
	if a.onChange != nil {
		a.onChange()
	}
	return nil
}
