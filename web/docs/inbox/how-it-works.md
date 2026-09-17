---
icon: lucide/workflow
description: The basic model behind workspaces, flows, feeds, actions, and notifications.
---

# How Hive works

Hive collects work from external sources and routes it into local feeds.

## The model

- A **source** reads from a provider, a feed, a command, or a local webhook.
- A **flow** connects sources to filters and destinations.
- A **workspace** is the Inbox view of one flow.
- A **feed** is a list of items produced by a flow.
- An **action** starts an agent session, runs a command, publishes a message, or copies text.
- A **notify** node raises a system banner or an in-app toast.

Hive stores item history and read state locally. Changing a flow recalculates feed membership without losing that history.

## Work with feed items

Use **Feed options** to sort a feed, refresh its sources, mark its items as read, or select multiple items.

In selection mode, choose items and select **Create session**. Hive opens one editable New Session form with context from every selected item in feed order. Items can come from different repositories; choose the repository in the form when Hive cannot derive one shared repository. After creation, the session appears in each selected item's detail pane.

Selections remain active while you search, filter, sort, or cancel the New Session form. Select **Cancel** in the selection bar to clear them.

## The app areas

Inbox is one of Hive's three main areas. See the [feature overview](../getting-started/features.md) for the core Inbox, Code, and Chats capabilities.

## Configure Hive

Use the flow editor and Settings screens, edit the YAML files directly, or ask the built-in **Hive** workspace in Chats.

- [Sources](sources.md) lists supported providers and generic inputs.
- [Flows](flows.md) covers the graph and editor.
- [Actions](actions.md) covers reusable commands.
- [Settings](../configuration/settings.md) lists configuration locations and categories.
