// Package ingest is the pipeline's producer side: the poll loop that turns
// configured source connectors into event_log rows, and the resolver that
// turns the current flow set into the connector instances it drives.
//
// Delivery — reading the log, routing it, committing a consumer's offset —
// belongs to internal/app/runtime; this package only appends.
package ingest

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// Msg is the pipeline's generic log record. It is store.Msg verbatim — a
// connector builds one per item and Producer appends it as-is, so there is no
// separate wire type to keep in sync.
type Msg = store.Msg

// Sources is what a producer tick needs from the connector registry. Declared
// here rather than exported from the registry because a package's dependency
// is exactly the methods it calls.
type Sources interface {
	// PullInstances is every enabled pull-mode connector instance across the
	// current flow set. It is called once per tick — rather than fixed at
	// construction — so a source node added to or removed from a flow takes
	// effect without a restart.
	PullInstances() []connector.Instance
	// Prefetch offers the tick's instances to any connector that declared
	// CapBatchPrefetch, before any of them is drained.
	Prefetch(ctx context.Context, instances []connector.Instance) error
}

// FlowLister is the subset of *flow.FlowStore this package needs: the current
// set of loaded flows.
type FlowLister interface {
	List() []flow.Flow
}

// Appender is the subset of *store.DB a Producer needs.
type Appender interface {
	IngestObservation(ctx context.Context, classifier store.Classifier, p store.IngestObservationParams) (store.IngestResult, error)
	AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []store.SnapshotItem) (offset int64, err error)
	ListActiveSourceHeadKeys(ctx context.Context, id store.SourceIdentity) ([]string, error)
	SourceHeadPayload(ctx context.Context, topic, key string) ([]byte, error)
	DeleteSourceHead(ctx context.Context, topic, key string) error
}
