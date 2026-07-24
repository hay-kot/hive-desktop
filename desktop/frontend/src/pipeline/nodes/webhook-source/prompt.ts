// Builds the paste-ready LLM prompt for authoring a function node that
// reshapes this webhook's payloads — the node-scoped sibling of
// lib/flowPrompt.ts's whole-flow prompt. It embeds everything an agent needs
// with no other context: the function node contract, the invariants the
// commit protocol imposes (only Payload may change), the feed-item target
// shape, and the endpoint's last captured delivery as the concrete input
// sample.

export interface TransformPromptOptions {
  /** The webhook-source node's configured path (labels the prompt). */
  path: string
  /** Last captured request body (raw JSON) — the concrete input sample. */
  sample?: string
}

const FEED_ITEM_SHAPE = `{
  "id": "unique-stable-id",        // string, required — item identity
  "kind": "PR" | "Issue" | string, // optional — the item's type label, and what
                                   // actions target via applies_to; omitted
                                   // means the kind "Item"
  "repo": "owner/name",            // string — repo line in the feed row
  "title": "human readable title", // string, required for a useful row
  "url": "https://…",              // string — opened by the o/Enter shortcut
  "num": 123,                      // number, optional — item number badge
  "author": "login",               // string, optional
  "body": "longer text",           // string, optional — detail pane snippet
  "labels": ["a", "b"],            // string[], optional
  "state": "open" | "resolved" | "closed" | "done" | string, // optional — resolved/closed/done archive the item; a later non-terminal state resurfaces it
  "updatedAt": 1712345678901       // unix milliseconds, optional
}`

export function buildTransformPrompt(options: TransformPromptOptions): string {
  const sample = options.sample?.trim()
  const lines: string[] = []

  lines.push(`Write the body of a Hive Desktop pipeline "function" node that transforms webhook payloads arriving on the "${options.path}" endpoint.`)
  lines.push('')
  lines.push('Contract — the code you write is the body of:')
  lines.push('')
  lines.push('    function on_message(msg, node, state) { /* your code */ }')
  lines.push('')
  lines.push('- `msg.Payload` is the delivered webhook JSON (already parsed — an object/array, not a string).')
  lines.push('- Return `msg` to pass it downstream, or `null` to drop it.')
  lines.push('- Reshape by REPLACING the payload: `return { ...msg, Payload: mapped }`.')
  lines.push('- CRITICAL: never change `msg.Key`, `msg.Topic`, `msg.SourceKind`, or `msg.SourceScope` — feed membership resolves by them, and altering them stalls the flow\'s commit.')
  lines.push('- No network, no imports; plain synchronous JavaScript. It runs in a sandboxed Web Worker with a 5s default timeout.')
  lines.push('')
  lines.push('Target payload shape — the canonical item contract (docs/decisions/0008) Hive Desktop feeds render richly:')
  lines.push('')
  lines.push('```jsonc')
  lines.push(FEED_ITEM_SHAPE)
  lines.push('```')
  lines.push('')
  lines.push('Map as many fields as the input supports; `id`, `title`, and `url` matter most. Keep any extra source fields you think downstream nodes might filter on — unknown fields are allowed.')
  lines.push('')
  if (sample) {
    lines.push('A real payload captured from this endpoint:')
    lines.push('')
    lines.push('```json')
    lines.push(sample)
    lines.push('```')
  } else {
    lines.push('No payload has been captured from this endpoint yet. Here is a sample payload the sender will POST:')
    lines.push('')
    lines.push('```json')
    lines.push('<paste a sample payload here>')
    lines.push('```')
  }
  lines.push('')
  lines.push('Reply with ONLY the JavaScript statements for the on_message body — no function wrapper, no markdown fences, no commentary.')

  return lines.join('\n')
}
