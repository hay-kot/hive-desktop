package ingest

import "github.com/hay-kot/hive-desktop/internal/app/observe"

var tracer = observe.Tracer("/internal/app/ingest")

// Attribute keys for the tick and its per-source children. These are span
// attributes rather than metric labels, which is why a source id — unbounded,
// because a user names it — is safe here.
const (
	attrForced    = "ingest.forced"
	attrSources   = "ingest.sources"
	attrDrained   = "ingest.drained"
	attrFailed    = "ingest.failed"
	attrAppended  = "ingest.appended"
	attrSourceID  = "ingest.source.id"
	attrSourceKnd = "ingest.source.kind"
	attrTopic     = "ingest.source.topic"
)
