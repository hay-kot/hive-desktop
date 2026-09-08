---
icon: lucide/bot
description: Run coding agents in named workspaces with selected skills and MCP servers.
---

# Agent workspaces

The **Chats** area runs a coding agent in a persistent workspace. Open it with <kbd>g</kbd> then <kbd>a</kbd>.

Chats run in tmux, so they can continue when you switch views or restart Hive.

## Requirements

- tmux 3.2 or newer;
- a supported agent CLI on your `PATH`.

Hive currently supports Claude Code and Codex. See [Troubleshooting](../getting-started/troubleshooting.md#a-coding-agent-is-not-found) if Hive cannot find one.

## The Hive workspace

Hive creates a built-in workspace named **Hive**. Use it to configure flows, sources, actions, settings, shortcuts, and other workspaces.

Open the workspace and describe the change you want. Its shipped skills know the local config paths and the app's current schemas. The default approval mode asks before running changes.

## Create a workspace

Use **New workspace** in Chats. A workspace chooses:

- a name;
- Claude Code or Codex;
- an approval mode;
- skill packages;
- MCP servers.

The matching manifest looks like this:

```yaml
version: 3
name: Home Assistant
agent: claude
autonomy: ask
mcps:
  - home-assistant
skills:
  - hive
```

Approval modes are:

- `ask` uses the agent's normal prompts;
- `auto` accepts edits and asks for higher-risk operations;
- `full` skips approval prompts.

Use `full` only for a workspace whose tools and instructions you trust.

## Workspace files

Workspaces live under `~/.config/hive/desktop/workspaces/` by default. **Settings ▸ Chats** shows the current root. To move it, set `agent_workspaces.dir` in `settings.yaml` and restart Hive.

Each workspace has an `agent-workspace.yaml` manifest and an `AGENTS.md` instruction file. Hive generates agent-specific configuration when the workspace opens. Put lasting instructions in `AGENTS.md` because generated files are replaced.

## Skills

A workspace selects skill packages, such as the built-in `hive` package. Packages can include shipped skills and custom skills from the shared workspace folder.

The built-in Hive package covers:

- flows and sources;
- actions and quick terminals;
- settings and keybindings;
- agent workspaces;
- the Hive MCP server.

## MCP servers

Hive includes these MCP entries:

- **Hive Desktop** for reading and configuring the running app. It requires the local HTTP server, which is enabled by default.
- **Hive Canvas** for markdown and HTML output beside a chat.
- **Playwright** for browser automation. It requires Node.js and a Playwright browser.
- **Chrome DevTools** for a running Chrome browser. It requires Node.js 20.19 or newer and Chrome.

You can add HTTP, SSE, and stdio servers in the workspace MCP library. Secrets in custom server configuration can use `env:`, `file:`, or `op://` references.

## Use a chat in Code

Pin a chat to the Code session tree when you want it beside repository terminals. It attaches like any other tmux session.
