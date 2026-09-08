---
icon: lucide/rocket
description: Install Hive Desktop and create your first feed.
---

# Getting Started

Hive Desktop collects work from supported sources and routes it into local feeds. Flows decide which items enter a feed, show a notification, or run an action.

## Install

<div class="hive-downloads" data-hive-downloads markdown="1">

Direct downloads come from the release manifest. Use the install script below if they do not appear.

</div>

The macOS download is a `.dmg`: open it and drag **Hive** to Applications. The Linux downloads are tarballs: extract one and put the `hive-desktop` binary on your `PATH`.

### Install from the terminal

The install script picks the right build, verifies its checksum, installs the app, and adds the `hive` command to your `PATH`.

```sh
curl -fsSL https://hivedesktop.com/install.sh | bash
```

=== "macOS"

    Installs `/Applications/Hive.app`, or `~/Applications` when the first is not writable.

=== "Linux"

    Installs the binary under `~/.local/share/hive`. Set `HIVE_HOME` to choose another directory.

To inspect the installer first, omit `| bash`. Add `-s -- --channel dev` after `bash` to install the dev channel, and set `HIVE_BIN_DIR` to choose where the `hive` symlink goes.

Hive updates itself. You can change the release channel under **Settings ▸ About**. See [Settings](../configuration/settings.md#updates).

## First run

1. Create a workspace.
2. Connect GitHub, or skip it and connect another [source](../inbox/sources.md).
3. Allow notifications if you want system banners.
4. Open a feed and select an item.

These pages cover each step:

- [Sign in to GitHub](sign-in.md)
- [Turn on notifications](notifications.md)
- [See your first items](first-feed.md)

## Configure Hive

Use the flow editor and Settings screens for normal configuration. You can also edit the YAML files under `~/.config/hive/desktop/`.

The built-in **Hive** workspace in **Chats** can create flows, connect generic sources, define actions, and change settings. It requires a supported coding agent CLI on your `PATH`.

Start with:

- [Sources](../inbox/sources.md)
- [Flows](../inbox/flows.md)
- [Actions](../inbox/actions.md)
- [Keyboard shortcuts](../configuration/keybindings.md)
