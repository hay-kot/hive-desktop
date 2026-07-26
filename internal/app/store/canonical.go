package store

import (
	"encoding/json"
	"strings"
)

// CanonicalFields is the one shared decode for the canonical top-level
// id/kind/state strings of an item payload (docs/decisions/0008). Every
// reader of the canonical contract goes through it — the action-item decode
// in dispatch and the webhook classifier in the webhook connector — so the
// string-or-number id rule has exactly one implementation.
//
// A payload that is not a JSON object, or that omits a field or gives it a
// non-string value, yields "" for that field rather than an error: the
// canonical fields are optional by contract.
func CanonicalFields(payload []byte) (id, kind, state string) {
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
