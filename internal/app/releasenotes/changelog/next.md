---
summary: "Terminal mode and the Agents area arrive, Grafana and exec join the connectors, and the app ships a signed macOS installer."
---

## Added

- **Terminal mode** (experimental): tmux-backed sessions rendered by a GPU
  atlas renderer, with searchable scrollback replay on attach, clickable
  links, and pop-up terminals that open straight into a TUI from a chord.
  Typography — family, weight, line height, letter spacing — is configurable.
- **The Agents area** (experimental): workspaces, tmux-backed chats, and an
  MCP catalogue. A chat can be pinned into the Code view's session tree to
  stay in reach while you work.
- **Grafana connector** for metrics, alerts, and IRM alert groups, with
  Alertmanager filtering pushed server-side so large instances stay
  responsive.
- **`sources.exec` connector** — run a command on the poll tick and ingest its
  stdout as a snapshot.
- **Gitea connector** for issues, pull requests, and notifications from a Gitea
  or Forgejo instance, configured with typed filters rather than a query
  string. Items land in the feed alongside GitHub's and route through the same
  filters and actions.
- **Generic webhook source**, with an uploaded image usable as its feed mark.
- **A signed, notarized macOS `.dmg` installer** beside the update zip, and
  Linux releases for amd64 and arm64.
- **Pick the app's fonts from your installed families.** Appearance lists the
  fonts on your machine for the UI and monospace faces, not just the bundled
  Inter and JetBrains Mono.
- **Hive sessions from the app** — create one from a feed item or on its own,
  manage them from the UI, and see which session an inbox item started.
- **Dry-run a flow** against supplied input without touching live state.
- **Actions declare inputs** and render them into their templates, alongside a
  richer default catalog and a native clipboard action type.
- **A notify terminal node**, so a feed can notify on new items.
- **In-app problem reporting** to a private bucket, and a documentation site
  with search on the landing page.
- **`!` in the command palette runs a shell command** in the Code view — the
  rest of the line opens a window on the attached session and types it there,
  so it runs in that checkout and the shell outlives it.
- **The command palette knows where you are.** Code view lists the attached
  session's windows and operations, every other session as an attach row, and
  the configured session and window actions — none of which were reachable
  from it before.
- **Your configured actions are in the palette.** The selected inbox item's
  actions are listed under its own reference, and running one opens its input
  form exactly as the detail pane's cards do.
- **A `+` on a repository in the session tree** starts a session in it,
  replacing the session count under the pointer. A workspace in the Agents
  sidebar has the same `+`, and it starts a chat outright rather than opening
  the new-chat form.
- **Drag a profile tile to reorder the rail** (or move the focused tile with
  Alt+Up/Down). The order is saved as `profiles.order` in `settings.yaml`;
  profiles you never move stay alphabetical behind the ones you place.
- **An optional status bar above a session** (Settings ▸ Terminal), carrying
  its branch, uncommitted and unpushed state, lines changed, and its pull
  request's review and CI status — plus the open-in-editor and show-in-Finder
  buttons a chat already had.

## Changed

- Settings are sectioned by the surface they change, and the navigation is
  grouped by the app's modes.
- Auto-update and publishing moved to R2 channel manifests.
- The Activity view is a ledger, the inbox trash was redesigned, and the feed
  gained date groups, hover actions, and mark-all-as-read.
- One source message is split into per-entity feed items rather than one
  combined row.
- The New Session form preselects the agent `hive` would actually run, and the
  dialog opens on ⌘N.
- The command palette no longer lists rows that cannot run where you are
  standing — feed commands over a terminal, feeds and themes outside the
  inbox — and ranks a strong title match above an early group.
- Quick terminal launchers are scoped to the session they run in rather than
  shared across every session.
- **The Agents sidebar lists every chat under its workspace**, instead of a
  workspace list filtering a flat list of chats below it. Workspaces fold and
  unfold, and stay that way; ones with a running agent open on their own. It
  reads like the feed sidebar now — one line, one icon per row — and the
  divider between the two old lists is gone.
- Every integration card in Settings wears its mark on the same ground, rather than three of them on a white tile and three on the app's own.
- The app icon is the four-node hive mark.
- The theme is persisted in `settings.yaml` instead of webview localStorage.

## Fixed

- Grafana alerts render as alerts, not as a bare heading and a timestamp. A
  firing alert carries the `Alert` kind, a body with its description and the
  value that tripped it, a link to the rule it was raised from, and its folder
  as the container; alerts from one rule on several instances are separated by
  their instance. IRM alert groups get the same kind and a body of their own.
  Both nodes now put string tags in the canonical `labels` and carry the raw
  label map as `alertLabels` — a `function` node reading `labels` as a map
  wants `alertLabels` instead. Alerts already in the inbox keep the old payload
  until they next change.
- A source badge showing an image — Grafana, or a webhook or command source
  with an uploaded mark — filled the badge edge to edge instead of sitting
  inset like the glyph marks beside it.
- Pasting into a terminal pane is sent as a paste instead of one Enter per
  line, which no longer fires half a script on the way in.
- A terminal recovers from broker overflow in place; the view is no longer
  torn down and rebuilt underneath you.
- tmux spawns in the resolved environment, so agent sessions launch.
- A dead terminal session is presented as not started rather than as an error.
- GitHub absence confirmation is batched, fixing the rate-limit N+1.
- Empty-scope inbox rows no longer wedge the flow consumer.
- The Linux build no longer depends on the BSD-only `syscall.Getsid`.
- A node editor in the flows canvas keeps what you typed when the flow
  reloads underneath it, instead of reverting and letting Save write the
  pre-edit values back.
