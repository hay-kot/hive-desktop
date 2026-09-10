---
summary: ""
---

## Added

- **Opening a terminal is one trace.** With `telemetry` on, an attach reports
  the tmux handshake, the size vote, the window list and the pane capture as
  separate steps, so a terminal that was slow to open resolves to the step that
  took the time.
