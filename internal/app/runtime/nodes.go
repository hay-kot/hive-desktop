package runtime

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// behavior is what a node type does when a message reaches it. Exactly one of
// the three fields is set.
//
// This is the runtime half of the node-type declaration whose config half is
// flow.registry: adding a node type is one line here and one line there, and
// Run never branches on a type string. A behavior with none of the three set
// is a node the engine does not know how to execute, which Run reports rather
// than silently dropping.
type behavior struct {
	// relay marks a node the core ingests for. The producer and the webhook
	// listener append its observations to the event log, so at run time it
	// only passes what was routed to it on to its wires.
	relay bool
	// sinks derives the committed side effects one message reaching this
	// terminal produces. Terminals have no outputs, so this is where a
	// message's journey ends.
	sinks func(flowID, nodeID string, cfg flow.NodeConfig, msg store.Msg) []store.Output
	// processor builds this node's per-instance transformer. It is called once
	// per node when a Runner is built.
	processor func(r *Runner, nodeID string, cfg flow.NodeConfig) (processor, error)
}

// processor transforms one message into port-indexed outputs. A nil result
// (or one whose ports are all empty) discards the message.
type processor interface {
	process(ctx context.Context, msg store.Msg) ([][]store.Msg, error)
	// reset drops whatever state the processor accumulated for its node,
	// so the next message starts clean. The engine calls it after a timeout —
	// the "terminate, respawn" the browser gave a wedged worker.
	reset()
	// close releases the processor's resources.
	close()
}

// behaviors is the runtime registry, keyed by the same type strings
// flow.registry uses.
var behaviors = map[string]behavior{
	"github-source":  {relay: true},
	"webhook-source": {relay: true},
	"github-filter":  {processor: newFilterNode},
	"function":       {processor: newFunctionNode},
	"feed":           {sinks: feedSinks},
	"action":         {sinks: actionSinks},
	"notify":         {sinks: notifySinks},
}

// feedSinks claims immutable inbox membership for the arriving item, and —
// when the feed is one that interrupts — raises the same notify output a
// notify node does, targeting the feed's own id so both deliver through one
// executor.
//
// A snapshot re-states every current item on every poll. It must therefore
// reconcile membership and nothing else: notifying from one would re-announce
// the whole feed every tick.
func feedSinks(flowID, nodeID string, cfg flow.NodeConfig, msg store.Msg) []store.Output {
	target := flowID + "/" + nodeID
	output := store.Output{
		Sink:        store.Sink{Kind: store.SinkKindFeed, TargetID: target},
		Key:         msg.Key,
		SourceTopic: msg.Topic,
		SourceKind:  msg.SourceKind,
		SourceScope: msg.SourceScope,
	}

	config, _ := cfg.(*flow.FeedConfig)
	if config == nil || config.Notify == nil {
		return []store.Output{output}
	}
	return []store.Output{output, {
		Sink:          store.Sink{Kind: store.SinkKindNotify, TargetID: target},
		Key:           msg.Key,
		OccurrenceKey: msg.OccurrenceKey,
		Payload:       msg.Payload,
		SourceTopic:   msg.Topic,
		SourceKind:    msg.SourceKind,
		SourceScope:   msg.SourceScope,
	}}
}

// actionSinks enqueues an output_command against the referenced actions.yml
// action. Unlike a feed or notify output it carries no source identity: an
// action runs over the payload, and its dedup key is the message's own
// occurrence key.
func actionSinks(_, _ string, cfg flow.NodeConfig, msg store.Msg) []store.Output {
	config, ok := cfg.(*flow.ActionConfig)
	if !ok {
		return nil
	}
	return []store.Output{{
		Sink:          store.Sink{Kind: store.SinkKindAction, TargetID: config.Action},
		OccurrenceKey: msg.OccurrenceKey,
		Payload:       msg.Payload,
		SourceTopic:   "",
	}}
}

// notifySinks raises a notification for whatever is routed to it. The output
// carries the message's source identity as well as its payload so the backend
// can resolve the inbox row behind it and a clicked notification can reveal
// that item.
func notifySinks(flowID, nodeID string, _ flow.NodeConfig, msg store.Msg) []store.Output {
	return []store.Output{{
		Sink:          store.Sink{Kind: store.SinkKindNotify, TargetID: flowID + "/" + nodeID},
		Key:           msg.Key,
		OccurrenceKey: msg.OccurrenceKey,
		Payload:       msg.Payload,
		SourceTopic:   msg.Topic,
		SourceKind:    msg.SourceKind,
		SourceScope:   msg.SourceScope,
	}}
}
