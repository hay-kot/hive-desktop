package store

import (
	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

var tracer = observe.Tracer("/internal/app/store")

// This package spans batch boundaries only, never individual statements.
//
// A per-statement span was tried and removed. It failed the volume rule
// (ADR a-span-is-a-trigger-or-a-wait-and-its-count-per-trigger-is-bounded-by-configuration): CommitBatch issues several statements for every output in
// its batch and IngestObservation runs once per ingested item, so the span
// count scaled with how much data arrived rather than with configuration. One
// poll tick produced spans in the hundreds, which buries the tick it was
// supposed to explain. The row counts that matter are attributes on the spans
// below.
