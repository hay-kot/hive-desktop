package webhook

import (
	"strconv"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// terminalStates are the canonical `state` values that end a webhook item's
// lifecycle. Comparison is case-insensitive; any other or absent state keeps
// the item active — a stateless webhook behaves as manual triage only.
var terminalStates = map[string]bool{"resolved": true, "closed": true, "done": true}

// decodeState extracts the canonical top-level `state` string from a delivery
// payload: lowercased and trimmed; "" for non-object payloads or a
// missing/non-string state. Delegates to store.CanonicalFields — no second
// copy of the canonical-field parsing.
func decodeState(payload []byte) string {
	_, _, state := store.CanonicalFields(payload)
	return strings.ToLower(strings.TrimSpace(state))
}

// titleCase renders a canonical state string as an event summary.
func titleCase(value string) string {
	if value == "" {
		return "State changed"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

// classifier is the source-side classifier for webhook observations. It reads
// the canonical top-level `state` and maps it to lifecycle exactly like the
// GitHub classifier: a first delivery is "received" (terminal on arrival
// stays unarchived, matching GitHub); a delivery that newly enters a terminal
// state system-archives with the state as both the event kind and the archive
// reason; a delivery that leaves a terminal state resurfaces as "reopened";
// anything else is "updated" activity.
//
// An unchanged re-delivery never reaches classification — IngestObservation
// skips it on the source-head comparison. The occurrence key is per delivery
// so downstream action dedup fires once per change.
type classifier struct{}

var _ store.Classifier = classifier{}

func (classifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	state := decodeState(current.Payload)
	curTerminal := terminalStates[state]
	lifecycle := store.LifecycleActive
	if curTerminal {
		lifecycle = store.LifecycleTerminal
	}
	out := store.Classification{
		Kind:          "updated",
		Transition:    store.TransitionNone,
		Attention:     store.AttentionActivity,
		Lifecycle:     lifecycle,
		SourceState:   state,
		OccurrenceKey: current.ExternalID + "@" + strconv.FormatInt(current.ObservedAt, 10),
		Summary:       current.Title,
	}
	if previous == nil {
		out.Kind = "received"
		return out
	}
	prevTerminal := terminalStates[decodeState(previous.Payload)]
	switch {
	case !prevTerminal && curTerminal:
		out.Kind, out.Summary, out.Transition, out.ArchivedReason = state, titleCase(state), store.TransitionEnteredTerminal, state
	case prevTerminal && !curTerminal:
		out.Kind, out.Summary, out.Transition = "reopened", "Reopened", store.TransitionLeftTerminal
	}
	return out
}
