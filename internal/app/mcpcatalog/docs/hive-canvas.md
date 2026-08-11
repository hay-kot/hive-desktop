# Hive Canvas

The `hive-canvas` MCP server is the chat's output surface: its tools put
content in front of the user in a pane beside the conversation, in the Agents
area, while the session keeps running. It is the difference between describing
a document in terminal scrollback and handing the user one to read
(ADR the-canvas-is-a-per-chat-file-served-over-its-own-mcp-entry).

Like `hive-desktop`, the server is the running app itself — nothing to
install, nothing to fetch. It is a separate entry so a workspace can have a
canvas without granting the app-control tool set, and the other way around.

## What goes on a canvas

A canvas is an ordered list of blocks, one canvas per chat session:

- **markdown** — a title (optional) and a body, rendered as GitHub-flavored
  markdown. Raw HTML in the body is escaped, not rendered.
- **link** — a title and an `http`, `https`, or `mailto` URL, shown as an
  openable link.

Block ids are the agent's own: reusing an id updates that block in place,
which is how a status line is revised instead of duplicated.

## Tools

- `put_block` — create or replace one block.
- `remove_block` — remove one block by id.
- `clear_canvas` — remove every block; the canvas itself survives.
- `read_canvas` — read the canvas exactly as the user sees it.

Every tool takes a `session` id naming the chat whose canvas it touches. Hive
sets it in the launched process's environment as `HIVE_AGENT_SESSION`; a chat
launched before canvas support existed does not have the variable until it is
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
processes, and writes nothing outside the canvas pane the user is looking at.
