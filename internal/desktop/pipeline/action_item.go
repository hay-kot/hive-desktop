package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/actions"
)

// DecodedActionItem is the canonical action-item projection of a persisted
// inbox payload: identity, canonical kind, and the payload bytes handed to
// the output worker for template rendering. The desktop service deliberately
// never accepts this shape from a client.
type DecodedActionItem struct {
	ID      string
	Kind    string
	Payload []byte
}

// DecodeActionItem projects any source's persisted inbox payload into the
// canonical action item. Object payloads: `id` accepts a string or number;
// a missing/blank id is filled from the row's external id (re-marshaling
// only in that case); `kind` is the canonical top-level string. Non-object
// payloads (arrays, scalars) pass through untouched — ID comes from
// externalID, kind is empty, and no id injection is attempted. All other
// fields pass through untouched (grab bag preserved).
func DecodeActionItem(payload []byte, externalID string) (DecodedActionItem, error) {
	id, kind, _ := canonicalFields(payload)

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		// Non-object payload (array, scalar, null) or invalid JSON: pass
		// through untouched. canonicalFields already returned "" for id/kind
		// above. fields == nil also catches a literal `null` payload, which
		// unmarshals into a nil map without error.
		return DecodedActionItem{ID: externalID, Kind: kind, Payload: payload}, nil
	}

	if id != "" {
		return DecodedActionItem{ID: id, Kind: kind, Payload: payload}, nil
	}

	// Missing/blank id: inject externalID, re-marshaling only in this case.
	// json.Marshal on a map[string]json.RawMessage writes each value's raw
	// bytes verbatim, so every other field survives byte-for-byte.
	idBytes, err := json.Marshal(externalID)
	if err != nil {
		return DecodedActionItem{}, fmt.Errorf("encoding action item id: %w", err)
	}
	fields["id"] = idBytes
	encoded, err := json.Marshal(fields)
	if err != nil {
		return DecodedActionItem{}, fmt.Errorf("encoding action item: %w", err)
	}
	return DecodedActionItem{ID: externalID, Kind: kind, Payload: encoded}, nil
}

// canonicalFields is the one shared decode for the canonical top-level
// id/kind/state strings — reused by DecodeActionItem here and by Phase 3's
// decodeWebhookState, and consistent with webhookIdentity's string-or-number
// id rule, so the package grows no per-caller decode copies.
func canonicalFields(payload []byte) (id, kind, state string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", "", ""
	}

	if raw, ok := fields["id"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			id = strings.TrimSpace(s)
		} else {
			var n json.Number
			if err := json.Unmarshal(raw, &n); err == nil {
				id = n.String()
			}
		}
	}
	if raw, ok := fields["kind"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			kind = strings.TrimSpace(s)
		}
	}
	if raw, ok := fields["state"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			state = strings.TrimSpace(s)
		}
	}
	return id, kind, state
}

// RenderRepoTarget renders a launch-session action's repo_template over the
// SAME OutputData context the executor renders with (zero CommandID /
// IsRerun) and trims the result. Extracted from LaunchSessionExecutor —
// which now calls it — so the applicability probe and the executor can
// never drift: a template referencing .Raw or .Key probes exactly as it
// executes. Actions without a repo_template (interactive launch-session, or
// any non-launch-session type) render "" with no error — they impose no
// payload requirement.
func RenderRepoTarget(action actions.Action, key string, payload []byte) (string, error) {
	cfg, ok := action.Config.(*actions.LaunchSessionConfig)
	if !ok || cfg.RepoTemplate == "" {
		return "", nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	renderer := tmpl.New(tmpl.Config{})
	rendered, err := renderer.Render(cfg.RepoTemplate, OutputData{
		Key:     key,
		Payload: decoded,
		Raw:     json.RawMessage(payload),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(rendered), nil
}

// ActionApplicability reports whether action can run against item and, when
// it cannot, a human-readable reason (applies_to mismatch, blank repo
// render, or the underlying template render error) so the invoke path stays
// debuggable. The hard-requirement rule derives from
// actions.Action.HeadlessCapable() — non-blank repo_template ⇒ requires a
// renderable, non-blank repo target — keeping the knowledge of which action
// types impose payload requirements next to the type definitions in package
// actions. All other action types and templates impose no applicability
// requirement.
func ActionApplicability(action actions.Action, item DecodedActionItem) (ok bool, reason string) {
	if !actions.AppliesTo(action, item.Kind) {
		return false, fmt.Sprintf("action %q does not apply to kind %q", action.ID, item.Kind)
	}

	_, isLaunchSession := action.Config.(*actions.LaunchSessionConfig)
	if !isLaunchSession || !action.HeadlessCapable() {
		return true, ""
	}

	repo, err := RenderRepoTarget(action, item.ID, item.Payload)
	if err != nil {
		return false, fmt.Sprintf("action %q: repo_template: %v", action.ID, err)
	}
	if repo == "" {
		return false, fmt.Sprintf("action %q: repo_template rendered blank", action.ID)
	}
	return true, ""
}
