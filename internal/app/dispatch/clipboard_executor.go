package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
)

// RenderClipboardText renders a clipboard action's text_template over the item
// — the same OutputData context a shell action's command_template renders over,
// with the shq helper available — and trims it. A blank render is an error, not
// an empty copy.
//
// It is the single renderer both the detail-pane render-only path
// (InboxService.RenderClipboardAction) and ClipboardExecutor call, so the text
// a user copies can never drift from what a durable dispatch would produce.
func RenderClipboardText(action actions.Action, key string, payload []byte) (string, error) {
	cfg, ok := action.Config.(*actions.ClipboardConfig)
	if !ok {
		return "", fmt.Errorf("clipboard: action %q has config type %T", action.ID, action.Config)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("clipboard: decode payload: %w", err)
	}
	text, err := tmpl.New(tmpl.Config{}).Render(cfg.TextTemplate, OutputData{
		Key:     key,
		Payload: decoded,
		Raw:     json.RawMessage(payload),
	})
	if err != nil {
		return "", fmt.Errorf("clipboard: text_template: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("clipboard: text_template rendered blank")
	}
	return text, nil
}

// ClipboardExecutor renders a clipboard action's text_template and returns the
// text as its outcome. A clipboard action is detail-pane only and never
// enqueued as an output_command — the detail pane renders it non-durably via
// RenderClipboardText — so this executor is what keeps the dispatch contract
// complete (every catalog action type has one) and would render identically
// were a durable path ever to dispatch it.
type ClipboardExecutor struct{}

func NewClipboardExecutor() *ClipboardExecutor { return &ClipboardExecutor{} }

func (e *ClipboardExecutor) Execute(_ context.Context, action actions.Action, data OutputData, _ ActionInvocationInput) (ExecutionResult, error) {
	text, err := RenderClipboardText(action, data.Key, data.Raw)
	if err != nil {
		return ExecutionResult{}, err
	}
	return ExecutionResult{Attempted: true, Outcome: &ExecutionOutcome{Clipboard: &ClipboardExecutionOutcome{Text: text}}}, nil
}
