package pipeline

import "github.com/hay-kot/hive-desktop/internal/app/store"

// Re-export the adapter-neutral contract at the producer boundary. The leaf
// store package owns it because ingestion invokes classifiers in-tx.
type (
	Observation      = store.Observation
	Lifecycle        = store.Lifecycle
	Transition       = store.Transition
	Attention        = store.Attention
	ArchivedActor    = store.ArchivedActor
	Classification   = store.Classification
	Classifier       = store.Classifier
	AbsenceVerdict   = store.AbsenceVerdict
	AbsenceConfirmer = store.AbsenceConfirmer
	SourceAdapter    = store.SourceAdapter
)
