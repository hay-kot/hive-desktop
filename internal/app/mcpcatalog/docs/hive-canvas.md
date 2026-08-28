# Hive Canvas

The `hive-canvas` MCP server is the chat's output surface: its tools put
content in front of the user in a pane beside the conversation, in the Agents
area, while the session keeps running. It is the difference between describing
a document in terminal scrollback and handing the user one to read
(ADR canvases-are-named-files-in-the-workspace-folder-served-over-their-own-mcp-entry).

Like `hive-desktop`, the server is the running app itself — nothing to
install, nothing to fetch. It is a separate entry so a workspace can have a
canvas without granting the app-control tool set, and the other way around.

## What a canvas is

A canvas is a named artifact in the workspace: an ordered list of blocks,
saved as `canvases/<name>.json` in the workspace folder. A chat can make as
many as it needs — name them by artifact (`release-notes`, `perf-report`),
give each a display title, and they outlive the conversation that made them.
Blocks are:

- **markdown** — a title (optional) and a body, rendered as GitHub-flavored
  markdown. Raw HTML in the body is escaped, not rendered.
- **link** — a title and an `http`, `https`, or `mailto` URL, shown as an
  openable link.

Block ids are the agent's own: reusing an id updates that block in place,
which is how a status line is revised instead of duplicated; a `before`
anchor places or moves a block ahead of an existing one.

## Tools

- `put_block` — create or replace one block; the first write under a new
  canvas name creates that canvas. Answers with the canvas metadata and the
  stored block, never the whole surface.
- `put_blocks` — write a batch of blocks in one atomic call, for laying out
  a canvas whole instead of block by block.
- `remove_block` — remove one block by id.
- `clear_canvas` — remove every block; the canvas, its name and title survive.
- `delete_canvas` — remove a canvas entirely.
- `read_canvas` — read one canvas exactly as the user sees it, every block
  in order.
- `list_canvases` — every canvas in the workspace, including ones earlier
  chats made.
- `open_canvas` / `close_canvas` — ask to show or hide the pane beside this
  chat, optionally pinned to one canvas. Best-effort: it applies only while
  the user is viewing this chat, and there is no acknowledgment either way.
  Open when something is finished and worth looking at, not on every write —
  a write while the pane is closed already lights an unseen dot.

Every tool takes a `session` id naming the calling chat. Hive sets it in the
launched process's environment as `HIVE_AGENT_SESSION`; a chat launched
before canvas support existed does not have the variable until it is
relaunched.

## Launch

Hive reaches it over Streamable HTTP on the loopback server that also hosts
the webhook listener:

```
http://127.0.0.1:<port>/mcp/canvas
```

The port is allocated at startup, so this entry carries **no URL in the
registry** — the live address is resolved when the catalogue is rendered
(`Descriptor.RuntimeURL`), and the catalogue reports a problem instead when
the loopback server is disabled.

The server requires no token. It sits behind the loopback bind, spawns no
processes, and writes only under the workspace's `canvases/` directory.
