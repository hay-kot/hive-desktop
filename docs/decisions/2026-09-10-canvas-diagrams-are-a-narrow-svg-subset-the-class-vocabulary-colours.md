# Canvas diagrams are a narrow svg subset the class vocabulary colours

- **Status:** accepted
- **Date:** 2026-09-10

## Context

[ADR canvas-html-blocks-are-sanitized-in-go-and-styled-by-an-app-owned-class-vocabulary](2026-08-29-canvas-html-blocks-are-sanitized-in-go-and-styled-by-an-app-owned-class-vocabulary.md)
closed the layout gap and left `svg` out, because foreign content is where a
second parse disagrees with the first and the pane feeds sanitizer output to
`v-html`. It closed with "the classes an agent reaches for and cannot find
are the list of what to add". A live session found one: asked for a diagram,
the agent wrote `<style>` and `<svg>`, was refused both, and fell back to
`hv-card` boxes in an `hv-grid` with `→` characters between them (#429). That
reads as four steps side by side. Which node feeds which, which path returns,
which one is dashed because it is a read and not a write — the relationships
are the content, and none of them survive.

Structure had an answer and quantity has a proposed one (#349). Graphs had
neither, and unlike a chart, a diagram's layout is a judgement the agent is
making rather than something derivable from data a schema could carry.

`<style>` stays refused whatever happens to the parse concern: a rule in one
block is not scoped to that block, so it restyles every other block in the
pane.

## Decision

**Eleven svg elements join the element allowlist**: `svg`, `g`, `path`,
`rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `text`, `tspan`.
The rest stays out, each for its own reason. `desc`, `title` and
`foreignObject` are the integration points where a browser parses HTML again
inside foreign content — the mutation the original ADR was right about — so
they join `math` and `template` in `SkipElementsContent` and their character
data never reaches the pane. `script` and `style` execute. `use` and `image`
fetch. `defs` and `marker` need ids and `url()` references, a second name
space to be correct about, for an arrowhead a `polygon` already draws.

**Colour comes from the same class vocabulary, never from the markup.**
`fill`, `stroke`, `stroke-width` and `font-size` are refused as attributes.
Five role classes carry a diagram instead — `hv-node`, `hv-edge`, `hv-arrow`,
`hv-label` and `hv-dashed` — and the existing tones colour any of them, on
the element or on a `g` around a whole path. `hv-muted` and `hv-mono` already
work on a `text`. This is the html ADR's trade unchanged: a model that names
its own colours gets one of the two themes wrong by construction, and "it is
a local app" is not an answer to that.

**The agent states the coordinate system and the app states the size.** A
`viewBox` is required and `width`/`height` on the `svg` are refused, so
`canvas-html.css` scales every diagram to whatever width the user dragged the
pane to. Requiring it is a write-time refusal like any other: without one the
stylesheet has no aspect ratio, and the result is a diagram that renders
wrong rather than not at all.

**Every attribute is declared once, with the values it may carry.**
`htmlAttrs` grew from a name-to-elements map into a name-to-rule map holding
the pattern too, and both the bluemonday policy and `RejectedHTML` are built
from it. Coordinates are plain numbers in `viewBox` units — no percentages,
no units, no exponents. This closes a hole the old shape had: `colspan="one"`
was stripped on render and accepted on write, which is the silent drop the
whole design exists to prevent.

**`SanitizeHTML` spells `viewBox` back.** SVG attribute names are
case-sensitive and the HTML tokenizer folds them, so the policy emits
`viewbox`. A browser is meant to fold it back when it re-parses the markup as
foreign content; jsdom, which the frontend tests run on, does not. Rather
than depend on a re-parse to repair the output, the one camelCase name in the
vocabulary is restored on the way out.

**Rejected: a typed `diagram` block the app lays out.** It is the trade #349
takes for charts, and it is the right one there — the data is the content and
the drawing is derivable from it. A diagram is the other way round: the
layout is the judgement, and the schema would cap what can be drawn at
whatever it models.

## Consequences

- The sanitizer now has a foreign-content branch to stay correct about. The
  hostile-input table carries the svg mutations that matter —
  `foreignObject`, `desc`, `title`, CDATA, a wrapped `style` — with exact
  expected output, because a policy that started escaping instead of dropping
  would still pass a substring check.
- An arrowhead is geometry the agent computes. Markers would have been
  fewer characters to write and would have needed ids, `url()` references and
  `context-stroke` to take an edge's tone; a `polygon` needs none of them.
- The vocabulary is a published contract, so these five names are permanent
  in the same way the first sixteen are. Adding is free; removing is not.
- **An exported canvas does not keep its diagrams.** For the rest of the
  vocabulary an export loses the styling and keeps the content; svg has no
  content without the stylesheet, and its own defaults — fill black, stroke
  none — turn a diagram into black boxes with the edges missing, in the
  viewers that render raw svg at all. The doc page says so, and a canvas
  written to be exported should carry the same fact in prose. Making the
  export self-contained means either a second copy of the role rules in Go or
  a `style` element in the output, and neither is worth it for a path the
  pane is not.
