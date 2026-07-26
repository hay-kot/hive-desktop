# Feed

A **feed** node is a terminal (1 input, 0 outputs): every message routed here creates an unread item under this feed. A feed's durable identity is its flow-qualified node id (`<flowId>/<nodeId>`) — what membership claims are keyed on.

## Fields

- `icon` — optional. One glyph from a curated feed icon set (e.g. `git-branch`, `bell`, `star`, `rss`, `webhook`, `bug`); empty uses the default (`git-branch`).
- `description` — optional, up to 500 characters. Free-text context for what this feed collects — useful for explaining what an LLM-generated feed is for.
- `notify` — optional. A block shaped exactly like a [notify](notify.md) node's config (`title`, `body`, `severity`, `sound`). Its presence turns this feed into one that interrupts for new arrivals; its absence makes this a feed that's read at the reader's own pace.

`icon` and `description` are cosmetic only and never affect which items land in the feed.

## Behavior

A message routed here always lands — there's nothing downstream to wire. Items stay unread until triaged.

## Notifications

A feed with `notify` set carries the same title/body/severity/sound config a [notify](notify.md) node does, and delivers through the same executor. The difference is scope: a notify node interrupts for whatever the author routes to it, while a notifying feed interrupts for what *arrives in it*, and only when the arrival is genuinely new — an observation ingestion triaged as real activity that left the item unread. A poll re-observing an unchanged item, a trivial edit, or a PR closing out of the feed does not interrupt.

Route what deserves interrupting with a filter upstream: a `github-filter` on the `review_requested` reason feeding a notifying feed is the "tell me when I'm asked to review something" case, and is what a new workspace ships with.

Application notification settings always win: the kill switch silences the feed entirely, and the delivery preference decides whether a notification arrives as an OS banner or an in-app toast.
