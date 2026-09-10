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
- **html** — a title (optional) and a body of markup, for layout markdown
  cannot express: a row of stat tiles, a two-column comparison, a card grid,
  or a drawn diagram. See [Writing an html block](#writing-an-html-block).
- **link** — a title and an `http`, `https`, or `mailto` URL, shown as an
  openable link.

Block ids are the agent's own: reusing an id updates that block in place,
which is how a status line is revised instead of duplicated; a `before`
anchor places or moves a block ahead of an existing one.

## Writing an html block

You write the structure; Hive owns every colour, size and space. Pick class
names from the vocabulary below and the block inherits the app's theme, in
light and dark, now and after a restyle. **Never write a `style` attribute, a
colour, a pixel value, or a class from outside the vocabulary** — the write is
refused, naming what it did not recognise, rather than quietly rendering
something that looks broken to the user.

### Tags

Sectioning and text (`div`, `section`, `article`, `header`, `footer`, `aside`,
`figure`, `figcaption`, `h1`, `h2`, `h3`, `h4`, `h5`, `h6`, `p`, `span`,
`strong`, `em`, `b`, `i`, `u`, `s`, `small`, `mark`, `sub`, `sup`, `abbr`,
`q`, `time`, `br`, `hr`, `blockquote`), lists (`ul`, `ol`, `li`, `dl`, `dt`,
`dd`), code (`pre`, `code`, `kbd`, `samp`, `var`), tables (`table`, `thead`,
`tbody`, `tfoot`, `tr`, `th`, `td`, `caption`, `colgroup`, `col`), disclosure
(`details`, `summary`), `a` with an `http`, `https` or `mailto` href, and the
drawing tags in [Diagrams](#diagrams).

Everything else is refused: no `script`, `style`, `iframe`, `object`, `embed`,
`form` or form controls, no event handlers, no `style` attribute — and **no
`img`**, because a remote image URL in the app's window is a request to
whoever you named. Attributes are `class`, `href` on `a`, `colspan`/`rowspan`
and `scope` on cells, `open` on `details`, and the geometry attributes in
[Diagrams](#diagrams).

### Classes

**Layout** — `hv-stack` (vertical, evenly spaced), `hv-row` (horizontal,
wraps), `hv-grid` with one of `hv-cols-2`, `hv-cols-3`, `hv-cols-4` (equal
columns; they collapse to one when the pane is narrow, so pick for the
content, not for a width you cannot see).

**Containers** — `hv-card` (a bordered, raised box: one unit of content),
`hv-panel` (a flat tinted region: a grouped aside), `hv-callout` (a
left-ruled block for something the reader must not miss).

**Data** — `hv-stat` wrapping an `hv-stat-value` and an `hv-stat-label` is one
tile; `hv-kv` on a `<dl>` lays its `<dt>`/`<dd>` pairs out as an aligned
key/value table.

**Emphasis** — `hv-badge` (an inline pill), `hv-muted` (de-emphasised text),
`hv-mono` (monospace, for ids, shas and figures).

**Tones**, for `hv-callout`, `hv-badge` and the diagram roles — `hv-info`,
`hv-success`, `hv-warn`, `hv-error`, `hv-accent`. Without one, each is
neutral.

### Diagrams

Prose and boxes cannot say which node feeds which, or which path is a return
path. Draw that as an `svg`: you place the geometry, Hive picks the size and
every colour, exactly as it does for the classes above.

Rules that make a diagram survive the write:

- **A `viewBox` is required, and `width`/`height` on the `svg` are refused.**
  Draw in whatever coordinate system suits the picture; the pane scales it to
  whatever width the user dragged the pane to.
- **Never write a colour.** No `fill`, `stroke`, `stroke-width` or
  `font-size` attributes — pick a role class and a tone instead.
- Coordinates are plain numbers in `viewBox` units. No percentages, no `px`.

**Tags** — `svg`, `g` (a group, to move or tone several shapes at once),
`path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `text`
and `tspan`. There is no `defs`, `marker` or `use`: draw an arrowhead as a
`polygon`.

**Attributes** — `viewBox` on `svg`; `transform` on anything inside it
(`translate`, `scale`, `rotate`, `matrix`, `skewX`, `skewY`); `d` on `path`;
`points` on `polyline`/`polygon`; `x`/`y`/`width`/`height`/`rx`/`ry` on
`rect`; `cx`/`cy`/`r` on `circle`; `cx`/`cy`/`rx`/`ry` on `ellipse`;
`x1`/`y1`/`x2`/`y2` on `line`; `x`/`y`/`dx`/`dy`/`text-anchor` on
`text`/`tspan`.

**Roles** — `hv-node` (a filled, bordered shape: one box in the diagram),
`hv-edge` (a stroked connector), `hv-arrow` (a filled arrowhead or any other
solid mark), `hv-label` (text the reader reads first), and `hv-dashed`
alongside `hv-edge` for a path that is conditional, asynchronous, or a read
rather than a write. A tone on the element — or on a `g` around a whole path
— colours it; `hv-muted` and `hv-mono` work on `text` exactly as they do on a
`span`.

### Worked examples

A row of stat tiles:

```html
<div class="hv-grid hv-cols-3">
  <div class="hv-card hv-stat">
    <span class="hv-stat-value">1,284</span>
    <span class="hv-stat-label">Requests</span>
  </div>
  <div class="hv-card hv-stat">
    <span class="hv-stat-value">98.2%</span>
    <span class="hv-stat-label">Success</span>
  </div>
  <div class="hv-card hv-stat">
    <span class="hv-stat-value">412ms</span>
    <span class="hv-stat-label">p95</span>
  </div>
</div>
```

A before/after comparison, and a status line:

```html
<div class="hv-grid hv-cols-2">
  <section class="hv-panel">
    <h3>Before</h3>
    <p class="hv-mono">412ms</p>
  </section>
  <section class="hv-panel">
    <h3>After</h3>
    <p class="hv-mono">96ms</p>
  </section>
</div>
<p class="hv-row">
  <span class="hv-badge hv-success">passing</span>
  <span class="hv-badge hv-warn">2 flaky</span>
  <span class="hv-muted">last run 4m ago</span>
</p>
```

Facts, and something the reader must not miss:

```html
<dl class="hv-kv">
  <dt>Branch</dt><dd class="hv-mono">feat/canvas-html</dd>
  <dt>Base</dt><dd class="hv-mono">main</dd>
  <dt>Reviewer</dt><dd>unassigned</dd>
</dl>
<div class="hv-callout hv-error">
  <strong>Migration 0042 is not reversible.</strong>
  Take a backup before deploying.
</div>
```

A pipeline with a return path:

```html
<svg viewBox="0 0 300 120">
  <g class="hv-accent">
    <rect class="hv-node" x="4" y="10" width="90" height="40" rx="7" />
    <text class="hv-label" x="49" y="35" text-anchor="middle">Poller</text>
  </g>
  <rect class="hv-node" x="150" y="10" width="90" height="40" rx="7" />
  <text class="hv-label" x="195" y="35" text-anchor="middle">Store</text>

  <line class="hv-edge" x1="94" y1="30" x2="140" y2="30" />
  <polygon class="hv-arrow" points="140,26 149,30 140,34" />

  <path class="hv-edge hv-dashed" d="M195 50 L195 90 L49 90 L49 50" />
  <polygon class="hv-arrow" points="45,58 49,50 53,58" />
  <text class="hv-muted" x="122" y="105" text-anchor="middle">re-read on wake</text>
</svg>
```

An html block is exported as its markup, so a canvas saved to a file keeps
the structure and loses the styling — the `hv-` names mean nothing outside
Hive. **A diagram does not survive that at all**: svg with no stylesheet
behind it draws black boxes and no edges, so if the canvas is meant to be
exported, say the same thing in prose in a block beside it. Reach for
`markdown` for prose and `html` only when the layout is the point.

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
