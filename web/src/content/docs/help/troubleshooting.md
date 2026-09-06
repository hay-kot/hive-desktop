---
title: Troubleshooting
description: The failures people actually hit, what each one looks like, and the way out of it.
group: Help
order: 0
---

Most of these leave a line in the log, so when a symptom is not listed here,
start there: **Settings ▸ System ▸ Diagnostics** opens it, or read
`~/.local/share/hive/desktop/desktop.log` directly. Flow and action problems
also appear in the **Activity** view with the file and the error.

## tmux is not installed

**What breaks.** Code shows a notice that tmux is not installed instead of a
session list, chats in the Chats area cannot start, and the pop-up terminal
does not open. Feeds, actions, and notifications are unaffected.

**Why.** Terminal mode drives tmux's control mode, which arrived in tmux 3.2.
Hive looks for the binary on your login shell's PATH and in the usual
Homebrew, MacPorts, and Nix prefixes.

**Fix.** Install it (`brew install tmux`) and go back to Code. Hive does not
cache a failed lookup, so the mode comes up without a relaunch. If tmux is
installed somewhere unusual, point `paths.tmux` in
[settings.yaml](/docs/configuration/settings#paths) at the binary. An older
tmux reports its version in the same notice; upgrade it.

## A coding agent is not found

**What breaks.** Starting a chat in the Chats area fails, or a session an
action launched opens a window that exits immediately, because the `claude` or
`codex` binary the workspace or hive's spawn rules name is not there.

**Why.** Hive asks your login shell for its environment once at launch, so an
agent your terminal can run is one Hive can run, even when Hive was opened
from Finder or the Dock. The cases that still fail: the agent is installed but
its directory is added to PATH only in a file the login shell does not read;
the shell's startup file errors out before it gets there; or the profile was
edited after Hive started.

**Fix.** In a terminal, `which claude` should print a path. If it does, make
sure the PATH entry comes from `.zprofile`, `.zshrc`, or the equivalent for
your shell, then quit and reopen Hive so it re-reads the environment. If it
does not, install the agent. A workspace's `agent:` must be `claude` or
`codex`; anything else is rejected when the workspace opens.

## Notifications never appear as banners

**What breaks.** Flows with notify nodes run, the Activity view shows the
delivery, but nothing surfaces while Hive is in the background. In-app toasts
still work.

**Why.** On macOS the system permission was denied or never requested. Hive
asks once during first run and never re-prompts mid-use. **Settings ▸
Notifications ▸ System permission** shows the current state.

**Fix.** If the state is *Not requested*, click **Allow notifications** there.
If it is *Denied*, open macOS **System Settings ▸ Notifications**, find Hive,
and allow it; the app picks the change up without a relaunch. Then check the
master switch and delivery mode on the same settings page, since `app`
delivery never raises a banner by design.

## The GitHub code expired

**What breaks.** The connect screen reports that the sign-in code expired
before authorization.

**Why.** GitHub's device code is valid for a few minutes. If the browser tab
sat open longer than that before you approved, or you approved a code from an
earlier attempt, the exchange fails.

**Fix.** Click **Connect GitHub** again for a fresh code and approve it
promptly. If the device flow keeps failing, **Use a token instead** accepts a
classic personal access token with the `repo` and `notifications` scopes and
skips the browser entirely.

## The feed is empty after skipping GitHub

**What breaks.** You skipped the GitHub step during first run, connected
later under **Settings ▸ Integrations**, and the workspace still has no feeds.

**Why.** The starter feeds are seeded only when an account is connected
during first run. Connecting afterwards does not seed them
([issue #387](https://github.com/hay-kot/hive-desktop/issues/387) tracks
fixing that).

**Fix.** Either ask the **Hive** workspace under Chats to build the feeds you
want, or add them yourself: open the workspace's canvas, add a
`sources.github` node with your account and a query such as
`is:open is:pr review-requested:@me`, wire it to a `feed` node, and deploy.
[Flows](/docs/concepts/flows#a-feed-built-up) walks through it.

## A flow or actions.yml change did nothing

**What breaks.** You edited `flows/<id>.yaml` or `actions.yml` and the app
kept behaving as before.

**Why.** A file that fails to parse or validate is rejected as a whole and
the last good version stays in service. The schema is strict, so an unknown
key, a bare number where a duration string belongs, or a `launch-session`
action with a terminal target are all rejections.

**Fix.** The Activity view names the file and the error. Fix it and save; the
app reloads on its own.

## A webhook delivery is rejected or never arrives

**What breaks.** The sender gets a `401`, a connection refused, or a `202`
with nothing landing in the feed.

**Why and fix, by symptom.**

- **`401`**: the node has a `secret` and the request did not carry it in the
  `X-Hive-Secret` header, or carried a different value.
- **Connection refused**: the listener is off (`http.enabled: false`), or the
  port changed. With `http.port: 0` the OS picks a port at each launch; pin
  one under **Settings ▸ Integrations ▸ Webhooks** so a script can rely on it.
- **`202` but no item**: the node's `path` does not match the URL, the node
  or its flow is disabled, or nothing is wired from the node to a feed. The
  node editor shows the last captured delivery, which tells you whether the
  request reached the node at all.

## The terminal grid is smaller than the pane

**What breaks.** A session attached in Code draws in a box that does not fill
the pane, with a note above it.

**Why.** Another tmux client is attached to the same session and its size is
winning. This is tmux's `window-size` rule, not a bug.

**Fix.** Detach the other client, resize it, or set
`set -g window-size largest` in your `tmux.conf`.
[Terminal mode](/docs/concepts/terminal-mode#why-the-grid-is-sometimes-not-the-size-of-the-pane)
explains the rule.

## Still stuck

[Report a problem](/docs/help/reporting-a-problem) covers the in-app reporter
and where the log and database live, and issues are open on
[GitHub](https://github.com/hay-kot/hive-desktop/issues).
