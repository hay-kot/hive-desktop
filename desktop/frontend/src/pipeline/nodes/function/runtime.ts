// Worker-side ProcessorRuntime for the `function` node. Never imports vue or
// any DOM global (see __tests__/import-hygiene.spec.ts in ../../__tests__)
// — this module runs inside a Web Worker in production and can be loaded by
// the explicit InProcessTransport test implementation.
//
// on_message is the node's whole lifecycle: no start/stop hooks, so `msg` is
// always a real message.

import type { NodeContext, ProcessorRuntime } from '../../engine/transport'
import { compile, type Config } from './config'

const functionRuntime: ProcessorRuntime<Config> = {
  type: 'function',

  onMsg(msg, ctx: NodeContext<Config>) {
    const fn = compile(ctx.config.on_message)
    return fn(msg, ctx.config as unknown as Record<string, any>, ctx.state)
  },
}

export default functionRuntime
