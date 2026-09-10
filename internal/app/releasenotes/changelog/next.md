---
summary: ""
---

## Added

- **Opening a terminal is one trace.** With `telemetry` on, an attach reports
  the tmux handshake, the size vote, the window list and the pane capture as
  separate steps, so a terminal that was slow to open resolves to the step that
  took the time.

## Fixed

- **Grafana IRM alert groups show the active alert and its context.** Firing,
  acknowledged and silenced groups all reach the feed, and details include the
  source description and labels instead of only an alert count.
