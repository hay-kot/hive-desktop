---
summary: ""
---

## Added

- **A launch-session action can run a command after the session starts.** Give
  it a `post_hook` and it runs in the new checkout with your shell's `PATH` —
  `gh pr checkout {{ .Payload.num }} && zed .` puts you in the editor on the
  right branch. A hook that fails leaves the session alone and reports its
  output in the action's run log.

## Changed

- **The Tasks button has left the title bar.** Tasks is still a chord away —
  ⌘⇧T, `g t`, the command palette, or the Code view's status bar.
- **The bug-report button has left the title bar.** Report a problem still
  opens from ⌘⇧B, the command palette, Settings ▸ System, and About.
- **The Chats canvas opens from the title bar.** The canvas had its own button
  in the pane status bar; it now rides the title bar's right-panel toggle, the
  same control the Inbox detail pane uses. Its attention dot moved with it.
- **Activity reads more quietly.** Color in the ledger now marks severity only —
  red for a failure, amber for something the app did on its own. Sessions,
  actions and system events are no longer each given their own hue.
- **Activity opens as a dialog.** It used to be a full screen you navigated to
  and back from. It now opens over whatever you were reading, the way Tasks
  does, and closes on Escape, the backdrop, or its own X.

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
