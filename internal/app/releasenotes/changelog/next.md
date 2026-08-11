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

## Changed

- Settings are sectioned by the surface they change, and the navigation is
  grouped by the app's modes.
- Auto-update and publishing moved to R2 channel manifests.
- The Activity view is a ledger, the inbox trash was redesigned, and the feed
  gained date groups, hover actions, and mark-all-as-read.
- One source message is split into per-entity feed items rather than one
  combined row.
- The New Session form preselects the agent `hive` would actually run.
- Quick terminal launchers are scoped to the session they run in rather than
  shared across every session.
- The app icon is the four-node hive mark.
- The theme is persisted in `settings.yaml` instead of webview localStorage.

## Fixed

- Pasting into a terminal pane is sent as a paste instead of one Enter per
  line, which no longer fires half a script on the way in.
- A terminal recovers from broker overflow in place; the view is no longer
  torn down and rebuilt underneath you.
- tmux spawns in the resolved environment, so agent sessions launch.
- A dead terminal session is presented as not started rather than as an error.
- GitHub absence confirmation is batched, fixing the rate-limit N+1.
- Empty-scope inbox rows no longer wedge the flow consumer.
- The Linux build no longer depends on the BSD-only `syscall.Getsid`.
