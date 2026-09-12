---
kind: added
---

**Canvas blocks are no longer held to Hive's own class names.** An html
block can use any class, its own `style`, its own colours, and images. The
rules that remain are about what a block can reach, not how it looks: no
script, no event handlers, no embedded documents, and links only to `http`,
`https` or `mailto`. A block also cannot paint outside its pane. One
consequence to know: an image, or a `url()` in a style, fetches from
whatever host the agent named, each time you open the canvas.
