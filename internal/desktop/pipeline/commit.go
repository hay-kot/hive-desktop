package pipeline

import "github.com/hay-kot/hive-desktop/internal/desktop/pipeline/pipelinedb"

// Sink, Output, Discard, NodeRun, and CommitBatch are pipelinedb's commit
// protocol structs, re-exported verbatim under package pipeline — the same
// alias pattern as Msg above: pipelinedb owns the transactional
// implementation (see pipelinedb/commit.go), while callers outside this
// package (desktop/pipelineservice.go, the frontend graph runtime via Wails
// bindings) speak in terms of this package's names.
type (
	Sink        = pipelinedb.Sink
	Output      = pipelinedb.Output
	Discard     = pipelinedb.Discard
	CommitBatch = pipelinedb.CommitBatch

	// NodeRun mirrors pipelinedb.NodeRunView, which is named "View" only to
	// avoid colliding with sqlc's generated raw node_run row model (also
	// named NodeRun, in pipelinedb/models.go).
	NodeRun = pipelinedb.NodeRunView

	// NodeRunRecord mirrors pipelinedb.NodeRunRecord, the read-side shape
	// returned by NodeRuns (see pipelinedb/node_run.go) — NodeRun's write
	// shape plus EndedAt.
	NodeRunRecord = pipelinedb.NodeRunRecord

	// NotifyCommand mirrors pipelinedb.NotifyCommand, the durable payload a
	// notify terminal's output_command carries.
	NotifyCommand = pipelinedb.NotifyCommand
)

// Sink.Kind values.
const (
	SinkKindFeed   = pipelinedb.SinkKindFeed
	SinkKindAction = pipelinedb.SinkKindAction
	SinkKindNotify = pipelinedb.SinkKindNotify
)

// NotifyActionPrefix namespaces the synthetic action ids notify terminals
// enqueue under.
const NotifyActionPrefix = pipelinedb.NotifyActionPrefix

// NotifyActionID and NotifyActionTarget map between a notify node's
// flow-qualified id and the synthetic action id its queued commands carry.
var (
	NotifyActionID     = pipelinedb.NotifyActionID
	NotifyActionTarget = pipelinedb.NotifyActionTarget
)
