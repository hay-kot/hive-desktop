// Package pipeline is the desktop pipeline's producer side: the Source seam
// that turns a configured data source into event_log rows, and the Producer
// that drives it on a poll tick. Delivery (reading the log, committing a
// consumer's offset) is exposed to the frontend by desktop/pipelineservice.go;
// this package only appends.
package ingest

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// Msg is the pipeline's generic log record. It is store.Msg verbatim —
// Source implementations build one per item and Producer appends it as-is,
// so there is no separate wire type to keep in sync.
type Msg = store.Msg

// Source produces the current state of one data source as a sequence of
// Msg, calling emit once per item. Produce is called synchronously from a
// Producer tick and returns once every current item has been emitted, or
// once fetching or an emit call fails. A successful call is an authoritative
// snapshot, including an empty one: Producer records the complete emitted
// key/payload set after it returns. Implementations should set:
//   - Topic: "source:" + the source's stable ID, so consumers can filter by
//     source.
//   - Key: the item's stable identity, used by AppendIfChanged to skip
//     unchanged source values within the same topic.
//   - Payload: the item, JSON-encoded.
type Source interface {
	Produce(ctx context.Context, emit func(Msg) error) error
}

// SourceLister resolves the current set of sources to poll, keyed by a
// stable ID (used as the log topic and for logging). Producer calls it once
// per tick — rather than fixing the set at construction — so source nodes
// added to or removed from flows take effect without a restart.
type SourceLister func(ctx context.Context) (map[string]Source, error)

// SourceMetadata is optional source-side data needed at the ingestion
// boundary. Exported because connectors now live in their own packages: Go
// cannot satisfy an unexported interface method from another package, so an
// unexported MetadataSource would compile and never match, silently
// downgrading every connector to generic ingestion.
type SourceMetadata struct {
	ProfileID   string
	SourceKind  string
	SourceScope string
	Policy      store.ResurfacePolicy
}

// MetadataSource is implemented by sources that describe their own ingestion
// metadata. A source that does not implement it is ingested as SourceKind
// "generic".
type MetadataSource interface{ IngestMetadata() SourceMetadata }

// FlowLister is the subset of *flow.FlowStore this package needs: the
// current set of loaded flows. It is called once per tick rather than fixed
// at construction, so a flow added, edited or removed takes effect without a
// restart. Declared per consuming package: a package's dependency on the
// flow store is exactly the method it calls.
type FlowLister interface {
	List() []flow.Flow
}

// Appender is the subset of *store.DB a Producer needs.
type Appender interface {
	IngestObservation(ctx context.Context, classifier store.Classifier, p store.IngestObservationParams) (store.IngestResult, error)
	AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []store.SnapshotItem) (offset int64, err error)
	ListSourceHeadKeys(ctx context.Context, topic string) ([]string, error)
	SourceHeadPayload(ctx context.Context, topic, key string) ([]byte, error)
}
