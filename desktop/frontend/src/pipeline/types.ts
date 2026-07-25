// Wire types are re-exported from the generated Wails bindings so the wire
// contract has one source, mirroring src/types/feed.ts's convention. Engine
// and node code must import Msg/CommitBatch/Output/etc. from here, never
// from bindings/ directly.
//
// Every one of these is generated from internal/app/store, which owns the
// commit protocol, so the graph runtime uses that wire contract without local
// compatibility aliases. NodeRun is exported there as NodeRunView, named to
// avoid colliding with sqlc's raw node_run row model.
export type { CommitBatch, Msg, Output, Sink, Discard, FeedSnapshot } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/store/models'
export type { NodeRunView as NodeRun } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/store/models'

import type { Discard, FeedSnapshot, NodeRunView as NodeRun, Output } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/store/models'

// Flow model (TS). The engine operates on in-memory Flow objects supplied
// by the editor/session layer, which adapts the generated Wails flow model
// from Go's flows/*.yaml loader.
export interface FlowNode {
  id: string
  type: string
  /** Author-facing display name edited in the drawer; nodes without one fall back to their type's label. */
  name?: string
  disabled?: boolean
  config: Record<string, any>
}

// out defaults to 0 (single-output nodes never need to set it).
export interface Wire {
  from: string
  out?: number
  to: string
}

export interface Flow {
  id: string
  nodes: FlowNode[]
  wires: Wire[]
}

/**
 * CommitResult is runGraph's return shape. It mirrors CommitBatch field for
 * field (consumer/upToOffset/outputs/feedSnapshots/discards/nodeRuns) and is structurally
 * assignable to it — a caller can pass a CommitResult straight into the
 * generated Commit() binding — but is declared separately so the engine has
 * its own name to document rather than reusing the wire type as a return
 * type.
 */
export interface CommitResult {
  consumer: string
  /** Decimal event-log offset. Strings preserve SQLite int64 precision in JavaScript. */
  upToOffset: string
  outputs: Output[]
  feedSnapshots: FeedSnapshot[]
  discards: Discard[]
  nodeRuns: NodeRun[]
}
