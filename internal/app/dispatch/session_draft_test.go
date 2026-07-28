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
