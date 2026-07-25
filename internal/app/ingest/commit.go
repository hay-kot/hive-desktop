package ingest

import "github.com/hay-kot/hive-desktop/internal/app/store"

// Sink, Output, Discard, NodeRun, and CommitBatch are store's commit
// protocol structs, re-exported verbatim under package pipeline — the same
// alias pattern as Msg above: store owns the transactional
// implementation (see store/commit.go), while callers outside this
// package (desktop/pipelineservice.go, the frontend graph runtime via Wails
// bindings) speak in terms of this package's names.
type (
	Sink        = store.Sink
	Output      = store.Output
	Discard     = store.Discard
	CommitBatch = store.CommitBatch

	// NodeRun mirrors store.NodeRunView, which is named "View" only to
	// avoid colliding with sqlc's generated raw node_run row model (also
	// named NodeRun, in store/models.go).
	NodeRun = store.NodeRunView

	// NodeRunRecord mirrors store.NodeRunRecord, the read-side shape
	// returned by NodeRuns (see store/node_run.go) — NodeRun's write
	// shape plus EndedAt.
	NodeRunRecord = store.NodeRunRecord

	// NotifyCommand mirrors store.NotifyCommand, the durable payload a
	// notify terminal's output_command carries.
	NotifyCommand = store.NotifyCommand
)

// Sink.Kind values.
const (
	SinkKindFeed   = store.SinkKindFeed
	SinkKindAction = store.SinkKindAction
	SinkKindNotify = store.SinkKindNotify
)

// NotifyActionPrefix namespaces the synthetic action ids notify terminals
// enqueue under.
const NotifyActionPrefix = store.NotifyActionPrefix

// NotifyActionID and NotifyActionTarget map between a notify node's
// flow-qualified id and the synthetic action id its queued commands carry.
var (
	NotifyActionID     = store.NotifyActionID
	NotifyActionTarget = store.NotifyActionTarget
)
