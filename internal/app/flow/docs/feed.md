# Feed

A **feed** node is a terminal (1 input, 0 outputs): every message that arrives creates an unread inbox-item membership claim for this feed, rendered in the sidebar.

## Fields

- **Sidebar icon** — an optional glyph shown next to the feed in the sidebar tree. Chosen from a scoped set of feed-relevant icons; unset feeds use the default branch glyph.
- **Description** — optional context (up to 500 characters) surfaced as a tooltip when hovering the feed in the sidebar. Useful for explaining what an LLM-generated feed collects.
- **Notify on new items** — off by default. Turn it on to make this the feed you point at "things that need my attention right now": a new item raises a system notification, and clicking it opens that item.

The feed's durable id is the flow-qualified node id (`<flowId>/<nodeId>`); the icon and description are purely cosmetic and never affect which items land in the feed.

## Behavior

A message routed here always lands — there's nothing downstream to wire. New items are marked unread until the user reads them in the sidebar.

## Notifications

A notifying feed carries the same title/body/severity/sound config a [notify](notify.md) node does, and delivers through the same executor. The difference is scope: a notify node interrupts for whatever the author routes to it, while a feed interrupts for what *arrives in it*, and only when the arrival is genuinely new — an observation ingestion triaged as real activity that left the item unread. A poll re-observing an unchanged item, a trivial edit, or a PR closing out of the feed does not interrupt.

Route what deserves interrupting with a filter upstream: a `github-filter` on the `review_requested` reason feeding a notifying feed is the "tell me when I'm asked to review something" case, and is what a new workspace ships with.

Application notification settings always win: the kill switch silences the feed entirely, and the delivery preference decides whether a notification arrives as an OS banner or an in-app toast.
