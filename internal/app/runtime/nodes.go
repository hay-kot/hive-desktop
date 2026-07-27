package runtime

import (
	"context"
	"encoding/json"
	"maps"
	"strings"

	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/sources"
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
	// snapshotReconciled marks a node whose committed sink output is
	// membership in a set that a source snapshot restates wholesale — a
	// feed. Run declares one reconciliation scope per (node, source) so the
	// commit can drop rows the latest snapshot no longer includes, even when
	// the snapshot is empty, and the engine derives a flow's feed ids from
	// the same flag to know what replay must recompute membership for and,
	// on disable, must remove. It is meaningless on a behavior with no sinks.
	snapshotReconciled bool
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
//
// Source types are not listed: a source is a relay by definition — the core
// ingests for it, so at run time it only forwards what was routed to it — and
// deriving them from the connector registry is what keeps adding a connector
// a change to internal/app/sources alone.
var behaviors = buildBehaviors(map[string]behavior{
	"github-filter": {processor: newFilterNode},
	"function":      {processor: newFunctionNode},
	"feed":          {sinks: feedSinks, snapshotReconciled: true},
	"action":        {sinks: actionSinks},
	"notify":        {sinks: notifySinks},
})

// buildBehaviors merges the behaviours declared here with the relay behaviour
// every registered source connector has. A collision panics for the same
// reason flow.buildRegistry's does: both maps are compile-time constants, so
// there is nothing to resolve to at run time.
func buildBehaviors(declared map[string]behavior) map[string]behavior {
	out := make(map[string]behavior, len(declared))
	maps.Copy(out, declared)
	for _, connectorType := range sources.Types() {
		if _, clash := out[connectorType]; clash {
			panic("runtime: source connector " + connectorType + " collides with a declared node type")
		}
		out[connectorType] = behavior{relay: true}
	}
	return out
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
//
// It also carries its own dedup key (NotifyDedupKey), so delivery collapses on
// what the author cares about rather than the occurrence key — which bumps on
// every meaningful update and would otherwise re-interrupt for each one.
func notifySinks(flowID, nodeID string, cfg flow.NodeConfig, msg store.Msg) []store.Output {
	config, _ := cfg.(*flow.NotifyConfig)
	return []store.Output{{
		Sink:           store.Sink{Kind: store.SinkKindNotify, TargetID: flowID + "/" + nodeID},
		Key:            msg.Key,
		OccurrenceKey:  msg.OccurrenceKey,
		NotifyDedupKey: notifyDedup(config, msg),
		Payload:        msg.Payload,
		SourceTopic:    msg.Topic,
		SourceKind:     msg.SourceKind,
		SourceScope:    msg.SourceScope,
	}}
}

// notifyDedup resolves a notify node's dedup key. With no `dedup` template the
// key is the item's source id, so a notify fires once per item and stays quiet
// through its later updates. With one, it is that template rendered over the
// message — fire once per distinct value. A template that errors or renders
// blank falls back to the item id rather than wedging the commit or collapsing
// every item onto one key; an item with no id (a function-synthesized message)
// leaves the key empty for commit.go's occurrence/digest fallback.
func notifyDedup(cfg *flow.NotifyConfig, msg store.Msg) string {
	if cfg == nil || cfg.Dedup == "" {
		return msg.Key
	}
	data := struct {
		Key     string
		Payload map[string]any
	}{Key: msg.Key}
	if len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &data.Payload)
	}
	rendered, err := tmpl.New(tmpl.Config{}).Render(cfg.Dedup, data)
	if err != nil {
		return msg.Key
	}
	if rendered = strings.TrimSpace(rendered); rendered != "" {
		return rendered
	}
	return msg.Key
}
