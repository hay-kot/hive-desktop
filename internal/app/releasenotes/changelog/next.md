---
summary: ""
---

## Fixed

- **The agent the New Session form shows is the agent that runs.** With
  `HIVE_DEFAULT_AGENT` set, the form preselected it correctly but the launch
  ignored it and started the configured `agents.default` instead, so the
  session came up on an agent you had not picked.
- **Adding a tab to a session that is not running now says so.** Pressing `+`
  on a session you have not started reported a failure to create a window,
  which named the wrong cause -- the window was never the problem. It now
  tells you the session is not running, and the failure is written to the log
  instead of vanishing.
- **A tab created while the window list was refreshing no longer disappears.**
  A refresh that had started before the tab existed treated it as closed, so
  the tab dropped out of the sidebar and renaming, closing or selecting it
  failed until the next refresh came round.
