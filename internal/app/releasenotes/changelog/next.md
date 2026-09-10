---
summary: ""
---

## Added

- **Opening a terminal is one trace.** With `telemetry` on, an attach reports
  the tmux handshake, the size vote, the window list and the pane capture as
  separate steps, so a terminal that was slow to open resolves to the step that
  took the time.
- **Every source node has a poll interval.** Grafana, GitHub, PostHog and
  Gitea sources take an `interval` — `1h`, `15m` — the same way command
  sources always have. Leave it empty to fetch every poll. The floor rounds up
  to the next tick, and a manual refresh ignores it.

## Fixed

- **Grafana IRM alert groups show the active alert and its context.** Firing,
  acknowledged and silenced groups all reach the feed, and details include the
  source description and labels instead of only an alert count.
- **Refresh now fetches.** The feed's Refresh button and `r` re-read the
  database and nothing else, so a source was only ever as fresh as the last
  poll tick. Both now fetch from every configured source first, and a spinner
  in the feed header says while it is happening.
- **Saving a flow runs it.** An added or edited source node used to produce
  nothing until the next poll tick, up to a minute later, which read as a flow
  that did not work. A deploy now fetches immediately.
