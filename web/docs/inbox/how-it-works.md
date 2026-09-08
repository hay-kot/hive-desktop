---
icon: lucide/workflow
description: The basic model behind workspaces, flows, feeds, actions, and notifications.
---

# How Hive works

Hive collects work from external sources and routes it into local feeds.

## The model

- A **source** reads from a provider, a command, or a local webhook.
- A **flow** connects sources to filters and destinations.
- A **workspace** is the Inbox view of one flow.
- A **feed** is a list of items produced by a flow.
- An **action** starts an agent session, runs a command, publishes a message, or copies text.
- A **notify** node raises a system banner or an in-app toast.

Hive stores item history and read state locally. Changing a flow recalculates feed membership without losing that history.

## The app areas

- **Inbox** contains workspaces, feeds, and item details.
- **Code** attaches to tmux sessions started by Hive.
- **Chats** runs coding agents in named workspaces with selected skills and MCP servers.

## Configure Hive

Use the flow editor and Settings screens, edit the YAML files directly, or ask the built-in **Hive** workspace in Chats.

- [Sources](sources.md) lists supported providers and generic inputs.
- [Flows](flows.md) covers the graph and editor.
- [Actions](actions.md) covers reusable commands.
- [Settings](../configuration/settings.md) lists configuration locations and categories.
