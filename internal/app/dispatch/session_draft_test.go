package dispatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSessionDraft_DerivesCloneableRepository(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		payload string
		want    string
	}{
		{"owner/name becomes an https clone url", "https://github.com/acme/site/issues/1", `{"repo":"acme/site"}`, "https://github.com/acme/site.git"},
		{"host comes from the item url", "https://git.example.com/acme/site/-/issues/1", `{"repo":"acme/site"}`, "https://git.example.com/acme/site.git"},
		{"an existing remote url passes through", "https://github.com/acme/site/issues/1", `{"repo":"git@github.com:acme/site.git"}`, "git@github.com:acme/site.git"},
		{"no repo yields empty", "https://github.com/acme/site/issues/1", `{}`, ""},
		{"no host yields empty rather than a broken clone target", "", `{"repo":"acme/site"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft, err := RenderSessionDraft("Fix it", tt.url, []byte(tt.payload))
			require.NoError(t, err)
			assert.Equal(t, tt.want, draft.Repository)
		})
	}
}

func TestRenderSessionDraft_NameAndPrompt(t *testing.T) {
	draft, err := RenderSessionDraft("Fix the crash", "https://github.com/acme/site/issues/1",
		[]byte(`{"repo":"acme/site","body":"Steps to repro"}`))
	require.NoError(t, err)
	assert.Equal(t, "fix-the-crash", draft.Name)
	assert.Contains(t, draft.Prompt, "Fix the crash")
	assert.Contains(t, draft.Prompt, "Steps to repro")
	assert.Contains(t, draft.Prompt, "https://github.com/acme/site/issues/1")
}

func TestSessionDraftMetadataRoundTrip(t *testing.T) {
	draft := SessionDraft{
		Repository: "https://github.com/acme/site.git",
		Name:       "fix-crash",
		Prompt:     "Fix the crash",
		Agent:      "claude",
		ItemID:     42,
		Failure: &SessionCreateFailure{
			Reason: "clone repository: git clone: exec git: exit status 1",
			Step:   "Cloning repository...",
			Output: "Clone strategy: full\nCloning repository...",
		},
	}

	meta := SessionDraftMetadata(draft)
	assert.Equal(t, RetryKindSessionCreate, meta[RetryMetadataKey])
	assert.NotContains(t, meta, "output", "a hook's output does not belong in an audit row")

	got, ok := SessionDraftFromMetadata(meta)
	require.True(t, ok)
	assert.Equal(t, draft.Repository, got.Repository)
	assert.Equal(t, draft.Name, got.Name)
	assert.Equal(t, draft.Prompt, got.Prompt)
	assert.Equal(t, draft.Agent, got.Agent)
	assert.Equal(t, int64(42), got.ItemID)
	require.NotNil(t, got.Failure)
	assert.Equal(t, draft.Failure.Reason, got.Failure.Reason)
	assert.Equal(t, draft.Failure.Step, got.Failure.Step)
	assert.Empty(t, got.Failure.Output, "the tail is in the ERR line, not on the row")
}

// A blank form was never submitted, so a row that claims one prefills nothing.
func TestSessionDraftFromMetadataRejectsWhatItDidNotWrite(t *testing.T) {
	for name, meta := range map[string]map[string]string{
		"nil":           nil,
		"empty":         {},
		"another kind":  {RetryMetadataKey: "something-else", "repository": "r"},
		"no marker":     {"repository": "r", "name": "n"},
		"no repository": {RetryMetadataKey: RetryKindSessionCreate, "name": "n"},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := SessionDraftFromMetadata(meta)
			assert.False(t, ok)
		})
	}
}

// "Default agent" round-trips as empty rather than as the configured default,
// which would change what the user picked.
func TestSessionDraftMetadataKeepsAnEmptyAgentEmpty(t *testing.T) {
	got, ok := SessionDraftFromMetadata(SessionDraftMetadata(SessionDraft{Repository: "r", Name: "n"}))
	require.True(t, ok)
	assert.Empty(t, got.Agent)
	assert.Zero(t, got.ItemID)
	assert.Nil(t, got.Failure, "no failure recorded means no panel to show")
}
