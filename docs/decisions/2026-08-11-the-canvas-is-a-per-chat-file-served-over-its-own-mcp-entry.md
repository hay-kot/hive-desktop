# The canvas is a per-chat file served over its own MCP entry

- **Status:** accepted
- **Date:** 2026-08-11

## Context

A workspace agent's only output channel is terminal scrollback, so anything it
produces that is meant to be looked at has to be described rather than shown
(#232). The MCP surface the canvas needs already exists in shape: `mcpsrv`
serves app tools over Streamable HTTP on the loopback server, and
`mcpcatalog`'s `RuntimeURL` mechanism resolves an app-hosted entry's address
at render time (ADR mcp-replaces-the-agent-facing-http-api).

What the canvas forced was four decisions the existing shape did not answer:
who owns a canvas, where its content lives, how the writing agent is
identified, and whether the tools join the `hive-desktop` entry.

## Decision

The canvas is a pane in the Agents area an agent fills with **blocks** —
`markdown` (rendered by the frontend's sanitizing GFM renderer; raw HTML is
escaped, never rendered) and `link` (http/https/mailto only). Block ids are
agent-chosen and upsert-in-place, so a block is revised rather than
duplicated. No HTML block kind: rendering agent-authored HTML in the webview
is a security decision this deliberately defers.

**One canvas per chat session, not per workspace.** A workspace runs several
concurrent sessions, which would fight over one surface. The canvas is keyed
by the `agent_workspace_session` record id, outlives the tmux session the way
the record does, dies with the record (`DeleteSession`/`DeleteWorkspace`
delete canvas files), and is browsable per workspace — the pane's picker lists
a workspace's canvases, labeling one whose record is gone by date.

**Content is a JSON file, not rows.** One file per canvas under
`<StateDir>/canvases/<workspace>/<session>.json`, written atomically
(temp+rename), owned by `internal/app/canvas`. Files keep the block model
free to change without migrations while it is young, are inspectable in
place, and delete as files; the immutable-migrations gate is exactly the
wrong cost for a shape this new.

**Identity rides the launch environment.** The session record exists before
launch, so `launchTerminal` injects `HIVE_AGENT_SESSION=<record id>` (and
`HIVE_AGENT_WORKSPACE`) via `tmux new-session -e` — set on the session, so
windows the agent opens later inherit it too. Every canvas tool takes that id
as its `session` argument, resolves it against the session store, and derives
the workspace from the record; an unknown id is `not_found`. A session
launched by a pre-canvas build lacks the variable until relaunched, and the
tool descriptions tell the agent to say so rather than guess.

**A separate `hive-canvas` catalogue entry, not more `hive-desktop` tools.**
Enabling an output surface must not require granting the app-control tool
set, and vice versa. The entry is the second app-hosted server, which is what
made `RuntimeURL` path-aware: `Descriptor.RuntimePath` joins the loopback
base per entry (`/mcp`, `/mcp/canvas`), and a pinning test holds the
catalogue's paths to the adapter's mount constants.

Delivery to the pane is the native wake-up pattern: content is stored state,
`events.CanvasUpdated` → coalesced `canvas:updated` → the pane re-reads.
The frontend reads over the token-guarded agents HTTP client; it has no
mutation surface at all — writes exist only as MCP tools, so the pane can
never race the agent.

## Consequences

- The threat model is unchanged: the canvas server keeps
  `mcpsrv`'s unauthenticated-behind-loopback posture, spawns nothing, and
  writes nothing outside the pane. Any local process can claim any session
  id — the same standing ADR mcp-replaces-the-agent-facing-http-api accepts
  for the whole surface. Codex's unbounded MCP wiring means an agent outside
  a Hive session can reach the canvas of a session whose id it knows; what
  such a write can do is put content in a pane the user reads.
- A canvas is machine-local (state dir), unlike the workspace directory it
  serves — two machines sharing a workspace root do not share canvases.
- `markdown-body` typography moved from DetailPane's scoped styles to a
  shared stylesheet, so the feed detail pane and the canvas render GFM
  identically.
- M1 is a viewport with agent read-back: the user cannot edit blocks. If
  user edits ever land, the read-back tools are where the agent would see
  them — the tool surface was shaped so that adding this does not change it.
