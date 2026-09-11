---
summary: ""
---

## Added

- **A canvas can carry a drawn diagram.** An agent writing an html block may
  now use svg — boxes, lines, paths, labels — so it can show you the shape of
  a pipeline instead of describing it. Roles named `hv-node`, `hv-edge`,
  `hv-arrow`, `hv-label` and `hv-dashed` take their colours from your theme,
  and a drawing scales to the pane.
- **Canvas blocks are no longer held to Hive's own class names.** An html
  block can use any class, its own `style`, its own colours, and images. The
  rules that remain are about what a block can reach, not how it looks: no
  script, no event handlers, no embedded documents, and links only to `http`,
  `https` or `mailto`. A block also cannot paint outside its pane. One
  consequence to know: an image, or a `url()` in a style, fetches from
  whatever host the agent named, each time you open the canvas.
