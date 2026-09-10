---
summary: ""
---

## Added

- **A canvas can carry a drawn diagram.** An agent writing an html block may
  now use a small set of svg tags — boxes, lines, paths, labels — and mark
  each shape with a role: `hv-node`, `hv-edge`, `hv-arrow`, `hv-label`, and
  `hv-dashed` for a path that is conditional or a read rather than a write.
  Hive picks every colour from your theme and scales the drawing to the pane,
  so a diagram follows a theme switch the way the rest of a canvas does. It
  writes the shape of a pipeline instead of describing it.
