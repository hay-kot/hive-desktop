package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/rs/zerolog"
)

// fakeSessionLauncher records every LaunchSession call.
type fakeSessionLauncher struct {
	calls []LaunchSessionRequest
	err   error
}

func (f *fakeSessionLauncher) LaunchSession(_ context.Context, req LaunchSessionRequest) (SessionExecutionOutcome, error) {
	f.calls = append(f.calls, req)
	return SessionExecutionOutcome{ID: "session-1", Name: req.Name}, f.err
}

type fakeSessionCreator struct {
	calls   []hive.CreateOptions
	err     error
	options hive.SessionLaunchOptions
}

func (f *fakeSessionCreator) SessionLaunchOptions(context.Context) (hive.SessionLaunchOptions, error) {
	return f.options, nil
}

func (f *fakeSessionCreator) ResolveSessionLaunchRepository(_ context.Context, remote string) (hive.SessionLaunchRepository, error) {
	for _, repo := range f.options.Repositories {
		if git.EquivalentRemote(repo.Remote, remote) {
			return repo, nil
		}
	}
	return hive.SessionLaunchRepository{Remote: remote}, nil
}

func (f *fakeSessionCreator) CreateSession(_ context.Context, opts hive.CreateOptions) (*session.Session, error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return nil, f.err
	}
	return &session.Session{ID: "session-1"}, nil
}

func TestLaunchSessionExecutor_RendersPromptAndRepoTemplates(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	exec := NewLaunchSessionExecutor(launcher)

	action := actions.Action{
		ID:   "spawn-review",
		Type: "launch-session",
		Config: &actions.LaunchSessionConfig{
			PromptTemplate: "Review {{ .Payload.title }} (key={{ .Key }})",
			Agent:          "claude",
			RepoTemplate:   " {{ .Payload.repo }} ",
		},
	}
	data := OutputData{
		Key:     "item-1",
		Payload: map[string]any{"title": "Fix the bug", "repo": "colonyops/hive"},
		Raw:     json.RawMessage(`{"title":"Fix the bug","repo":"colonyops/hive"}`),
	}

	_, err := exec.Execute(t.Context(), action, data, ActionInvocationInput{})
	require.NoError(t, err)

	require.Len(t, launcher.calls, 1)
	got := launcher.calls[0]
	assert.Equal(t, "spawn-review-item-1", got.Name)
	assert.Equal(t, "Review Fix the bug (key=item-1)", got.Prompt)
	assert.Equal(t, "claude", got.Agent)
	assert.Equal(t, "colonyops/hive", got.Repo)
}

func TestLaunchSessionExecutor_RerunUsesUniqueSessionName(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	exec := NewLaunchSessionExecutor(launcher)
	action := actions.Action{ID: "spawn-review", Type: "launch-session", Config: &actions.LaunchSessionConfig{
		PromptTemplate: "hi", RepoTemplate: "git@github.com:colonyops/hive.git",
	}}

	_, err := exec.Execute(t.Context(), action, OutputData{Key: "item-1", CommandID: 42, IsRerun: true, Payload: map[string]any{}, Raw: json.RawMessage(`{}`)}, ActionInvocationInput{Rerun: true})
	require.NoError(t, err)
	require.Equal(t, "spawn-review-item-1-rerun-42", launcher.calls[0].Name)
}

func TestLaunchSessionExecutor_ConfiguredRepoTemplateIgnoresInteractiveOverride(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	exec := NewLaunchSessionExecutor(launcher)
	action := actions.Action{ID: "spawn-review", Type: "launch-session", Config: &actions.LaunchSessionConfig{
		PromptTemplate: "hi", RepoTemplate: "git@github.com:colonyops/hive.git", Agent: "configured-agent",
	}}
	_, err := exec.Execute(t.Context(), action, OutputData{Key: "item-1", Payload: map[string]any{}, Raw: json.RawMessage(`{}`)}, ActionInvocationInput{Session: &SessionInvocationInput{
		Name: "frontend-name", Repository: "https://github.com/other/repo.git", Agent: "frontend-agent",
	}})
	require.NoError(t, err)
	require.Equal(t, LaunchSessionRequest{Name: "spawn-review-item-1", Prompt: "hi", Repo: "git@github.com:colonyops/hive.git", Agent: "configured-agent"}, launcher.calls[0])
}

func TestLaunchSessionExecutor_NoRepoTemplate_LeavesRepoEmpty(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	exec := NewLaunchSessionExecutor(launcher)

	action := actions.Action{
		ID:     "spawn-review",
		Type:   "launch-session",
		Config: &actions.LaunchSessionConfig{PromptTemplate: "hi"},
	}

	_, err := exec.Execute(t.Context(), action, OutputData{Key: "item-1", Payload: map[string]any{}}, ActionInvocationInput{Session: &SessionInvocationInput{Name: "spawn-review-item-1", Repository: "git@example/repo"}})
	require.NoError(t, err)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, "git@example/repo", launcher.calls[0].Repo)
}

func TestLaunchSessionExecutor_PropagatesLaunchFailure(t *testing.T) {
	launcher := &fakeSessionLauncher{err: errors.New("clone failed")}
	exec := NewLaunchSessionExecutor(launcher)
	action := actions.Action{ID: "spawn-review", Type: "launch-session", Config: &actions.LaunchSessionConfig{PromptTemplate: "hi"}}

	_, err := exec.Execute(t.Context(), action, OutputData{Key: "item-1", Payload: map[string]any{}}, ActionInvocationInput{Session: &SessionInvocationInput{Name: "spawn-review-item-1", Repository: "git@example/repo"}})
	require.ErrorIs(t, err, launcher.err)
	require.Len(t, launcher.calls, 1)
}

func TestLaunchSessionExecutor_WrongConfigType_IsError(t *testing.T) {
	exec := NewLaunchSessionExecutor(&fakeSessionLauncher{})
	action := actions.Action{ID: "x", Type: "launch-session", Config: &actions.ShellConfig{}}

	_, err := exec.Execute(t.Context(), action, OutputData{}, ActionInvocationInput{})
	require.Error(t, err)
}

func TestLaunchSessionExecutor_NilLauncherIsError(t *testing.T) {
	exec := NewLaunchSessionExecutor(nil)
	action := actions.Action{ID: "spawn-review", Type: "launch-session", Config: &actions.LaunchSessionConfig{PromptTemplate: "hi"}}
	_, err := exec.Execute(t.Context(), action, OutputData{Key: "item-1", Payload: map[string]any{}}, ActionInvocationInput{})
	require.Error(t, err)
}

func TestHiveSessionLauncher_MapsRequestToSessionService(t *testing.T) {
	creator := &fakeSessionCreator{}
	launcher := NewHiveSessionLauncher(creator)

	_, err := launcher.LaunchSession(t.Context(), LaunchSessionRequest{
		Name: "review-pr-1", Prompt: "Review this", Agent: "claude", Repo: "https://example.test/repo.git",
	})
	require.NoError(t, err)
	require.Equal(t, []hive.CreateOptions{{
		Name: "review-pr-1", Prompt: "Review this", AgentKey: "claude", Remote: "https://example.test/repo.git", Background: true,
	}}, creator.calls)
}

func TestHiveSessionLauncher_PrefersEquivalentConfiguredCheckout(t *testing.T) {
	creator := &fakeSessionCreator{options: hive.SessionLaunchOptions{Repositories: []hive.SessionLaunchRepository{{
		Name: "hive", Remote: "git@github.com:colonyops/hive.git", Source: "/work/hive",
	}}}}
	_, err := NewHiveSessionLauncher(creator).LaunchSession(t.Context(), LaunchSessionRequest{
		Name: "review-pr-1", Prompt: "Review this", Agent: "claude", Repo: "https://github.com/colonyops/hive.git",
	})
	require.NoError(t, err)
	require.Equal(t, hive.CreateOptions{
		Name: "review-pr-1", Prompt: "Review this", AgentKey: "claude", Remote: "git@github.com:colonyops/hive.git", Source: "/work/hive", Background: true,
	}, creator.calls[0])
}

func TestHiveSessionLauncher_PropagatesServiceFailure(t *testing.T) {
	creator := &fakeSessionCreator{err: errors.New("tmux unavailable")}
	_, err := NewHiveSessionLauncher(creator).LaunchSession(t.Context(), LaunchSessionRequest{Name: "review-pr-1"})
	require.ErrorIs(t, err, creator.err)
}

// fakeItemSessionLinker records the association the launcher persists.
type fakeItemSessionLinker struct {
	links map[string]store.ItemRef
	err   error
}

func (f *fakeItemSessionLinker) LinkItemSession(_ context.Context, sessionID string, ref store.ItemRef) error {
	if f.err != nil {
		return f.err
	}
	if f.links == nil {
		f.links = map[string]store.ItemRef{}
	}
	f.links[sessionID] = ref
	return nil
}

func TestHiveSessionLauncher_LinksTheCreatedSessionToItsItem(t *testing.T) {
	creator := &fakeSessionCreator{}
	linker := &fakeItemSessionLinker{}
	launcher := NewHiveSessionLauncher(creator)
	launcher.SetItemSessionLinker(linker, zerolog.Nop())
	ref := store.ItemRef{ProfileID: "p", SourceKind: "github", SourceScope: "acct", ExternalID: "acme/repo#1"}

	_, err := launcher.LaunchSession(t.Context(), LaunchSessionRequest{Name: "review-1", Prompt: "go", Repo: "r", Origin: ref})
	require.NoError(t, err)
	assert.Equal(t, map[string]store.ItemRef{"session-1": ref}, linker.links)
	// The item id also goes on the session as a hive tag, for a reader inside
	// hive. It is presentational and never read back.
	require.Len(t, creator.calls, 1)
	assert.Equal(t, []string{"acme/repo#1"}, creator.calls[0].Tags)
}

// A session with no item behind it — the blank New Session form, an action run
// from a terminal target — must not produce a link, or every such session
// would share one.
func TestHiveSessionLauncher_LinksNothingWithoutAnOrigin(t *testing.T) {
	creator := &fakeSessionCreator{}
	linker := &fakeItemSessionLinker{}
	launcher := NewHiveSessionLauncher(creator)
	launcher.SetItemSessionLinker(linker, zerolog.Nop())

	_, err := launcher.LaunchSession(t.Context(), LaunchSessionRequest{Name: "review-1", Prompt: "go", Repo: "r"})
	require.NoError(t, err)
	assert.Empty(t, linker.links)
	require.Len(t, creator.calls, 1)
	assert.Empty(t, creator.calls[0].Tags)
}

// The session exists either way, so reporting the launch as failed would be a
// lie — and would invite a retry that creates a second session.
func TestHiveSessionLauncher_ReportsSuccessWhenTheLinkCannotBeWritten(t *testing.T) {
	launcher := NewHiveSessionLauncher(&fakeSessionCreator{})
	launcher.SetItemSessionLinker(&fakeItemSessionLinker{err: errors.New("disk full")}, zerolog.Nop())

	outcome, err := launcher.LaunchSession(t.Context(), LaunchSessionRequest{
		Name: "review-1", Prompt: "go", Repo: "r",
		Origin: store.ItemRef{ProfileID: "p", ExternalID: "acme/repo#1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", outcome.ID)
}

// The flow-fired path: a command the engine enqueued carries the item it was
// routed from, and the executor hands it to the launcher.
func TestLaunchSessionExecutor_CarriesTheCommandsOriginToTheLauncher(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	exec := NewLaunchSessionExecutor(launcher)
	action := actions.Action{ID: "spawn-review", Type: "launch-session", Config: &actions.LaunchSessionConfig{
		PromptTemplate: "review", RepoTemplate: "acme/site",
	}}
	ref := store.ItemRef{ProfileID: "p", SourceKind: "github", SourceScope: "acct", ExternalID: "acme/site#81"}

	_, err := exec.Execute(t.Context(), action, OutputData{
		Key: "oc-1", Raw: json.RawMessage(`{}`), Payload: map[string]any{}, Origin: ref,
	}, ActionInvocationInput{})
	require.NoError(t, err)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, ref, launcher.calls[0].Origin)
}
