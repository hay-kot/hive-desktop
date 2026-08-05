package exec

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/sources/canonical"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// absence resolves every item that left the snapshot. A successful run's stdout
// is the complete current set by contract, so an item missing from it is gone —
// not merely unseen — and every verdict is terminal. This is the property the
// webhook bridge this node replaces could never have: a sender that stops
// sending says nothing about the item's fate.
//
// It only ever runs after a *successful* run: a non-zero exit or unparseable
// stdout fails Produce, and the producer skips absence confirmation entirely
// for a source whose snapshot is not authoritative.
type absence struct{}

var _ store.AbsenceConfirmer = absence{}

func (absence) ConfirmAbsence(_ context.Context, previous []store.Observation) (map[string]store.AbsenceVerdict, error) {
	verdicts := make(map[string]store.AbsenceVerdict, len(previous))
	for _, prev := range previous {
		resolved := prev
		resolved.Payload = canonical.WithState(prev.Payload, canonical.TerminalState)
		verdicts[prev.ExternalID] = store.AbsenceVerdict{Current: &resolved, Terminal: true}
	}
	return verdicts, nil
}
