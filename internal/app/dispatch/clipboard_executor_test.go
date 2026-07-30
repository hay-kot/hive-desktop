package dispatch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
)

func clipboardAction(template string) actions.Action {
	return actions.Action{ID: "copy", Type: "clipboard", Config: &actions.ClipboardConfig{TextTemplate: template}}
}

func TestClipboardExecutor_RendersTextAsOutcome(t *testing.T) {
	out, err := NewClipboardExecutor().Execute(
		t.Context(),
		clipboardAction("gh pr checkout {{ .Payload.num }} -R {{ .Payload.repo }}"),
		OutputData{Key: "pr-9", Raw: json.RawMessage(`{"num":9,"repo":"acme/app"}`)},
		ActionInvocationInput{},
	)
	require.NoError(t, err)
	require.NotNil(t, out.Outcome)
	require.NotNil(t, out.Outcome.Clipboard)
	assert.Equal(t, "gh pr checkout 9 -R acme/app", out.Outcome.Clipboard.Text)
}

func TestRenderClipboardText_ShqIsAvailable(t *testing.T) {
	text, err := RenderClipboardText(clipboardAction("{{ .Payload.title | shq }}"), "k", []byte(`{"title":"a b"}`), nil)
	require.NoError(t, err)
	assert.Equal(t, "'a b'", text)
}

func TestRenderClipboardText_BlankRenderIsError(t *testing.T) {
	_, err := RenderClipboardText(clipboardAction("{{ .Payload.empty }}"), "k", []byte(`{"empty":""}`), nil)
	require.Error(t, err)
}

func TestClipboardExecutor_WrongConfigType_IsError(t *testing.T) {
	_, err := NewClipboardExecutor().Execute(t.Context(), actions.Action{ID: "x", Type: "clipboard", Config: &actions.ShellConfig{}}, OutputData{}, ActionInvocationInput{})
	require.Error(t, err)
}
