// Parity against the Go engine.
//
// The fixtures under internal/app/runtime/testdata/parity are executed by
// BOTH engines: internal/app/runtime/parity_test.go runs them through the Go
// one, this spec runs the same files through this one, and each compares
// against the same expected commit. Neither engine can see the other's code —
// only the same inputs and the same answer — which is what makes this
// evidence rather than a restatement.
//
// This is the gate on deleting this engine. While both exist they must agree;
// a fixture that only one of them satisfies means the port is not done.
//
// Two fields are normalized away, and only two. durMs is wall-clock. err is
// the engine's own wording for a thrown value, and two different JavaScript
// implementations have no reason to phrase that identically — what has to
// match is *that* the node failed, which ok and the counters already say.

import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { runGraph } from '../runGraph'
import { InProcessTransport } from '../transport'
import { processorRegistry } from '../../processors'
import { flowFromWire, type WireFlow } from '../../lib/wireFlow'
import type { CommitResult, Msg } from '../../types'

const PARITY_DIR = join(dirname(fileURLToPath(import.meta.url)), '../../../../../../internal/app/runtime/testdata/parity')

interface Fixture {
  name: string
  why: string
  flow: WireFlow
  batch: Msg[]
  expected: Record<string, any>
}

function fixtures(): Array<{ file: string; fixture: Fixture }> {
  return readdirSync(PARITY_DIR)
    .filter((file) => file.endsWith('.json'))
    .sort()
    .map((file) => ({ file, fixture: JSON.parse(readFileSync(join(PARITY_DIR, file), 'utf8')) as Fixture }))
}

/** Renders a commit in the shape Go's json tags produce — omitempty included — so the two engines' results are comparable field for field. */
function normalize(batch: Partial<CommitResult> & Record<string, any>) {
  return {
    consumer: batch.consumer,
    upToOffset: batch.upToOffset,
    outputs: (batch.outputs ?? []).map((output: any) => {
      const out: Record<string, any> = {
        sink: { kind: output.sink.kind, targetId: output.sink.targetId },
        sourceTopic: output.sourceTopic ?? '',
      }
      if (output.key) out.key = output.key
      if (output.occurrenceKey) out.occurrenceKey = output.occurrenceKey
      if (output.payload !== undefined && output.payload !== null) out.payload = output.payload
      if (output.sourceKind) out.sourceKind = output.sourceKind
      if (output.sourceScope) out.sourceScope = output.sourceScope
      if (output.snapshotId) out.snapshotId = output.snapshotId
      return out
    }),
    feedSnapshots: (batch.feedSnapshots ?? []).map((s: any) => ({ feedId: s.feedId, sourceTopic: s.sourceTopic, snapshotId: s.snapshotId })),
    discards: (batch.discards ?? []).map((d: any) => ({ msgId: d.msgId, nodeId: d.nodeId })),
    nodeRuns: (batch.nodeRuns ?? []).map((run: any) => ({
      flowId: run.flowId,
      nodeId: run.nodeId,
      ok: run.ok,
      inCount: run.inCount,
      outCount: run.outCount,
      dropCount: run.dropCount,
      err: run.err ? '!' : '',
      durMs: 0,
    })),
  }
}

describe('engine parity with internal/app/runtime', () => {
  const all = fixtures()

  it('finds the shared fixtures', () => {
    expect(all.length).toBeGreaterThan(0)
  })

  for (const { file, fixture } of all) {
    it(`${file} — ${fixture.name}`, async () => {
      const flow = flowFromWire(fixture.flow)
      const transport = new InProcessTransport(processorRegistry)
      try {
        const result = await runGraph(flow, fixture.batch, transport)
        expect(normalize(result), fixture.why).toEqual(normalize(fixture.expected))
      } finally {
        transport.dispose()
      }
    })
  }
})
