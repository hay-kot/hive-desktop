package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// message is one message in flight through the graph: the wire message plus
// the snapshot it arrived as part of, if any. The snapshot context is
// deliberately engine-local — it decides routing and tagging and has no
// meaning once the batch is committed, so it never reaches store.Msg.
type message struct {
	msg store.Msg
	// snapshot is non-nil when this message was expanded out of a source
	// snapshot, and identifies the snapshot it came from.
	snapshot *snapshotContext
}

type snapshotContext struct {
	sourceTopic string
	snapshotID  string
}

// Run executes the flow over batch and returns the commit it is worth.
//
// Every input message is accounted for exactly once — as a tagged terminal
// output, as a discard, or (when a node failed) as an errored discard — so the
// consumer offset in the returned batch can always advance past the whole
// page. That is the invariant the commit protocol's idempotency rests on: a
// message the graph has no use for still moves the cursor.
//
// Run does not commit anything. A caller that wants the batch applied passes
// it to store.CommitBatch; a caller previewing a flow simply reads it.
func (r *Runner) Run(ctx context.Context, batch []store.Msg) (store.CommitBatch, error) {
	return r.run(ctx, batch, false)
}

// RunReplay recomputes membership with a fully inert KV — durable and
// overlay reads miss, staged writes are discarded — so replay stays a pure
// function of (current snapshots, current graph) and never suppresses items
// via dedup history. It resets the processors on the way out, so state
// mutated during the recompute never reaches the next live Run.
func (r *Runner) RunReplay(ctx context.Context, batch []store.Msg) (store.CommitBatch, error) {
	result, err := r.run(ctx, batch, true)
	r.resetProcessors()
	return result, err
}

func (r *Runner) run(ctx context.Context, batch []store.Msg, inert bool) (store.CommitBatch, error) {
	now := time.Now().UnixMilli()
	kv := newKVBuffer(r.opts.KV, r.flow.ID, now)
	if inert {
		kv = newInertKVBuffer(r.flow.ID, now)
	}
	state := &runState{
		runner:       r,
		pending:      map[string][]message{},
		runs:         map[string]*nodeRunAcc{},
		snapshotSeen: map[string]bool{},
		kv:           kv,
	}

	state.route(batch)
	if err := state.execute(ctx); err != nil {
		return store.CommitBatch{}, err
	}

	return store.CommitBatch{
		Consumer:      r.flow.ID,
		UpToOffset:    upToOffset(batch),
		Outputs:       state.outputs,
		FeedSnapshots: state.snapshots,
		Discards:      state.discards,
		NodeRuns:      state.nodeRuns(),
		KVMutations:   state.kv.mutations(),
	}, nil
}

// runState is one Run's working set. Every ordered field is built in a
// deterministic order — batch order for messages, topological order for
// nodes, flow declaration order for feeds — so two runs over the same input
// produce byte-identical batches.
type runState struct {
	runner  *Runner
	pending map[string][]message

	outputs   []store.Output
	discards  []store.Discard
	snapshots []store.FeedSnapshot
	// snapshotSeen dedupes feedSnapshots by (feed, source topic) while
	// snapshots keeps first-seen order.
	snapshotSeen map[string]bool

	// runs accumulates per-node counters; runOrder keeps the order nodes were
	// first touched, which is the topological order restricted to the nodes
	// that actually received a message.
	runs     map[string]*nodeRunAcc
	runOrder []string

	kv *kvBuffer
}

type nodeRunAcc struct {
	inCount   int
	outCount  int
	dropCount int
	ok        bool
	err       string
	dur       time.Duration
}

// route offers each input message to the entry nodes that accept it, expanding
// source snapshots into their items on the way.
func (s *runState) route(batch []store.Msg) {
	entries := s.runner.graph.Entries()

	for _, msg := range batch {
		matching := make([]string, 0, len(entries))
		for _, id := range entries {
			if s.runner.acceptsEntry(s.runner.graph.Node(id), msg) {
				matching = append(matching, id)
			}
		}
		if len(matching) == 0 {
			s.discards = append(s.discards, store.Discard{MsgID: msg.ID, NodeID: UnroutedNodeID})
			continue
		}

		if msg.Snapshot == nil {
			s.offer(matching, message{msg: msg})
			continue
		}

		// A snapshot restates the source's complete current item set, even
		// items whose payload did not change. Its items route normally, but
		// every feed in the flow also gets a reconciliation declaration so the
		// commit can atomically drop that source's rows that are no longer in
		// it — including when the set is empty, which is why an empty snapshot
		// is still a snapshot.
		snapshot := &snapshotContext{sourceTopic: msg.Topic, snapshotID: msg.ID}
		s.declareSnapshot(snapshot)
		for _, item := range msg.Snapshot {
			expanded := msg
			expanded.Key = item.Key
			expanded.Payload = item.Payload
			expanded.Snapshot = nil
			s.offer(matching, message{msg: expanded, snapshot: snapshot})
		}
	}
}

// declareSnapshot records one reconciliation scope per snapshot-reconciled
// node (a feed) for this source, keeping first-seen order.
func (s *runState) declareSnapshot(snapshot *snapshotContext) {
	for i := range s.runner.flow.Nodes {
		node := &s.runner.flow.Nodes[i]
		if !behaviors[node.Type].snapshotReconciled {
			continue
		}
		feedID := s.runner.flow.ID + "/" + node.ID
		key := feedID + "\x00" + snapshot.sourceTopic
		if s.snapshotSeen[key] {
			continue
		}
		s.snapshotSeen[key] = true
		s.snapshots = append(s.snapshots, store.FeedSnapshot{
			FeedID:      feedID,
			SourceTopic: snapshot.sourceTopic,
			SnapshotID:  snapshot.snapshotID,
		})
	}
}

// offer queues m at every matching entry node, giving each its own copy when
// there is more than one: two branches must never share a payload one of them
// can rewrite.
func (s *runState) offer(nodeIDs []string, m message) {
	for _, id := range nodeIDs {
		next := m
		if len(nodeIDs) > 1 {
			next.msg.Payload = clonePayload(m.msg.Payload)
		}
		s.pending[id] = append(s.pending[id], next)
	}
}

// execute walks the graph in topological order, running each node over
// whatever reached it.
func (s *runState) execute(ctx context.Context) error {
	for _, nodeID := range s.runner.graph.Order() {
		msgs := s.pending[nodeID]
		if len(msgs) == 0 {
			continue
		}
		node := s.runner.graph.Node(nodeID)
		run := s.acc(nodeID)

		for _, m := range msgs {
			run.inCount++

			if node.Disabled {
				s.drop(run, nodeID, m)
				continue
			}

			behavior := behaviors[node.Type]
			switch {
			case behavior.relay:
				s.forward(run, nodeID, 0, m)
			case behavior.sinks != nil:
				s.commitTerminal(run, node, m)
			default:
				if err := s.process(ctx, run, node, m); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// commitTerminal turns a message reaching a terminal into its committed
// outputs.
//
// Snapshot messages are the exception that shapes this: they re-state items
// the user has already seen, so they may only reconcile feed membership.
// Letting one through an action or notify terminal would enqueue an action —
// or re-announce an item — on every poll.
func (s *runState) commitTerminal(run *nodeRunAcc, node *flow.Node, m message) {
	outputs := behaviors[node.Type].sinks(s.runner.flow.ID, node.ID, node.Config, m.msg)

	if m.snapshot != nil {
		kept := outputs[:0]
		for _, output := range outputs {
			if output.Sink.Kind != store.SinkKindFeed {
				continue
			}
			output.SnapshotID = m.snapshot.snapshotID
			kept = append(kept, output)
		}
		outputs = kept
	}

	if len(outputs) == 0 {
		s.drop(run, node.ID, m)
		return
	}
	s.outputs = append(s.outputs, outputs...)
	run.outCount++
}

// process runs a node's processor over one message and forwards what it
// produced.
//
// A processor failure is the message's problem, not the flow's: the node is
// marked not-ok so the debug view shows it, the message is discarded, and the
// batch keeps going. A timeout additionally resets the node, because a node
// that did not return control cannot be trusted to hold sane state — the
// "terminate, respawn" the browser gave a wedged worker.
func (s *runState) process(ctx context.Context, run *nodeRunAcc, node *flow.Node, m message) error {
	proc := s.runner.procs[node.ID]
	if proc == nil {
		return errors.New("flow " + s.runner.flow.ID + ": node " + node.ID + ": no processor for type " + node.Type)
	}

	timeout := s.runner.timeouts[node.ID]
	staging := s.kv.node(node.ID)
	nodeCtx, cancel := context.WithTimeout(ctx, timeout)
	started := time.Now()
	produced, err := proc.process(nodeCtx, m.msg, staging)
	run.dur += time.Since(started)
	cancel()

	if err != nil {
		// Staged KV writes go down with the errored message: a kv.set
		// followed by a throw must persist nothing.
		run.ok = false
		run.err = err.Error()
		s.drop(run, node.ID, m)
		var scriptErr *ScriptError
		if errors.As(err, &scriptErr) && scriptErr.Kind == ScriptErrorTimeout {
			proc.reset()
		}
		return nil
	}

	staging.commit()

	if emptyPorts(produced) {
		s.drop(run, node.ID, m)
		return nil
	}

	for port, outs := range produced {
		for _, out := range outs {
			s.forward(run, node.ID, port, message{msg: out, snapshot: m.snapshot})
		}
	}
	return nil
}

// forward hands a message to every wire leaving one of a node's output ports.
// An unwired port is not an error — it is how a filter's fail port expresses
// "drop this" — but the message is still accounted for.
func (s *runState) forward(run *nodeRunAcc, nodeID string, port int, m message) {
	wires := s.runner.graph.Wires(nodeID, port)
	if len(wires) == 0 {
		s.drop(run, nodeID, m)
		return
	}
	run.outCount++
	for _, wire := range wires {
		next := m
		if len(wires) > 1 {
			next.msg.Payload = clonePayload(m.msg.Payload)
		}
		s.pending[wire.To] = append(s.pending[wire.To], next)
	}
}

func (s *runState) drop(run *nodeRunAcc, nodeID string, m message) {
	run.dropCount++
	s.discards = append(s.discards, store.Discard{MsgID: m.msg.ID, NodeID: nodeID})
}

func (s *runState) acc(nodeID string) *nodeRunAcc {
	if existing := s.runs[nodeID]; existing != nil {
		return existing
	}
	acc := &nodeRunAcc{ok: true}
	s.runs[nodeID] = acc
	s.runOrder = append(s.runOrder, nodeID)
	return acc
}

func (s *runState) nodeRuns() []store.NodeRunView {
	runs := make([]store.NodeRunView, 0, len(s.runOrder))
	for _, nodeID := range s.runOrder {
		acc := s.runs[nodeID]
		runs = append(runs, store.NodeRunView{
			FlowID:    s.runner.flow.ID,
			NodeID:    nodeID,
			OK:        acc.ok,
			InCount:   acc.inCount,
			OutCount:  acc.outCount,
			DropCount: acc.dropCount,
			Err:       acc.err,
			DurMs:     acc.dur.Milliseconds(),
		})
	}
	return runs
}

// acceptsEntry reports whether an entry node ingests this message. A source
// node only ever ingests its own flow-qualified topic, so two flows reading
// the same log never steal each other's messages. Any other entry node — a
// bare processor with no upstream source, which only a test builds — accepts
// whatever it is given.
func (r *Runner) acceptsEntry(node *flow.Node, msg store.Msg) bool {
	if node == nil {
		return false
	}
	if !behaviors[node.Type].relay {
		return true
	}
	return msg.Topic == "source:"+r.flow.ID+"/"+node.ID
}

// clonePayload gives a branch its own copy of a payload. Fan-out is the only
// place it is needed: one downstream branch rewriting a payload must never be
// visible to a sibling.
func clonePayload(payload json.RawMessage) json.RawMessage {
	if payload == nil {
		return nil
	}
	out := make(json.RawMessage, len(payload))
	copy(out, payload)
	return out
}

func emptyPorts(ports [][]store.Msg) bool {
	for _, port := range ports {
		if len(port) > 0 {
			return false
		}
	}
	return true
}

// upToOffset is the highest offset in the batch — the point the consumer may
// advance to once this batch commits. The log is read in ascending order, so
// this is normally the last message; taking the maximum rather than the last
// keeps the result correct for a caller that assembled a batch itself, which
// the replay protocol does.
//
// msg.ID stays a string end to end (see store.Msg), so every message's offset
// is parsed back out here; the batch sizes this runs over do not make it
// worth carrying a parallel unexported offset field just to skip a strconv
// call.
func upToOffset(batch []store.Msg) int64 {
	var highest int64
	for _, msg := range batch {
		offset, err := strconv.ParseInt(msg.ID, 10, 64)
		if err != nil {
			continue
		}
		if offset > highest {
			highest = offset
		}
	}
	return highest
}
