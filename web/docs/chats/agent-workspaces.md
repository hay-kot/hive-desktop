---
icon: lucide/bot
description: The Chats area. A workspace's manifest, its AGENTS.md, autonomy modes, skill packages, the MCP servers it can enable, and the Hive workspace that ships with the app.
---

# Agent workspaces

The Chats area. A workspace's manifest, its AGENTS.md, autonomy modes, skill packages, the MCP servers it can enable, and the Hive workspace that ships with the app.

An agent workspace is a named, durable directory where a CLI coding agent runs
against a purpose-built set of MCP tools, for work that has no repository:
configuring Hive itself, driving a browser, talking to a home automation
server. The **Chats** area (<kbd>g</kbd> then <kbd>a</kbd>) lists your
workspaces on the left and the chats running in each on the right. A chat is a
tmux session, so it keeps running when you look away and is still there after
a restart.

## What it needs

A workspace declares which agent runs it, and that agent's CLI has to be on
your PATH. Hive knows `claude` and `codex` today. It resolves PATH the way your
terminal would, by asking your login shell once at launch, so an agent your
shell can find is one Hive can find. [Troubleshooting](../getting-started/troubleshooting.md)
covers the case where it cannot.

## The Hive workspace

The first time the workspace root is created, Hive seeds one workspace named
**Hive**:

```yaml
version: 3
name: Hive
agent: claude
autonomy: ask
skills:
  - hive
```

Its `skills: [hive]` names the seeded **hive** skill package, which selects
every `hive-*` skill this build ships: flows, actions, settings, webhook
sources, agent workspaces, and the MCP server. A release that adds a skill
changes what this workspace carries with no edit to the manifest.

!!! tip "Use it to configure the app"
    This is the intended way to set Hive up when you would rather describe a
    change than write YAML. Open Chats, pick Hive, and ask for a feed, a filter,
    an action, or a shortcut. The agent edits the same files the rest of these
    docs describe, with your install's real paths, and the app reloads them on
    save. Autonomy is `ask`, so nothing runs without your confirmation.

Deleting it deletes it. Hive seeds the workspace only when it creates the root
for the first time, never on every launch.

## Where workspaces live

Every workspace is a directory under the workspace root, which is
`workspaces/` inside your config directory unless `agent_workspaces.dir` in
`settings.yaml` points somewhere else (iCloud Drive is the expected reason).

```
workspaces/
├── mcps.yaml                     # your own MCP server library
├── skills.yml                    # your skill package library
├── .shared/skills/<name>/SKILL.md  # skills you author
└── hive/                         # one workspace; the directory name is its identity
    ├── agent-workspace.yaml      # the manifest, hand-edited
    ├── AGENTS.md                 # the workspace's instructions, hand-edited
    ├── CLAUDE.md                 # generated
    ├── .mcp.json                 # generated
    ├── .codex/config.toml        # generated
    ├── .claude/skills/           # generated
    ├── .agents/skills/           # generated
    ├── docs/                     # generated, empty
    └── canvases/                 # written by the agent, left alone
```

The directory name is the workspace's whole identity; there is no separate id
field, so you create a workspace by creating the directory (the **New
workspace** button does the same). Opening a workspace regenerates everything
except the manifest, `AGENTS.md`, and `canvases/` from the manifest, so edits to
generated files are silently replaced on the next open. Prose about what the
workspace is for goes in `AGENTS.md`; the manifest has no `system_prompt`.

## The manifest

```yaml
version: 3
name: Home Assistant        # shown on the workspace card
agent: claude               # claude or codex
autonomy: ask               # ask (default), auto, or full
mcps:                       # MCP server ids this workspace enables
  - hive-desktop
  - playwright
  - home-assistant
skills:                     # skill package names from skills.yml
  - hive
```

`autonomy` is how much authority the workspace grants its agent:

- **`ask`** leaves every prompt to the agent CLI's own defaults. The area's
  approval indicator shows a chat waiting on you.
- **`auto`** accepts edits but still asks about anything riskier.
- **`full`** skips prompting entirely. Reserve it for a workspace you already
  trust completely.

`mcps` names entries from the shipped catalogue or from your own `mcps.yaml`;
an `mcps.yaml` entry can replace a shipped one of the same id. `skills` names
**packages**, never individual skills: `skills: [hive-mcp]` enables nothing
and is reported when the workspace opens.

## Skill packages

`skills.yml` defines the packages a workspace can enable as a unit:

```yaml
version: 1
packages:
  hive:
    title: Hive
    description: Configure Hive Desktop itself.
    include:
      - "hive-*"
  infra:
    title: Infrastructure
    include: ["terraform-*", "k8s-*", "runbook"]
    exclude: ["terraform-experimental"]
```

Patterns are globs over skill *names*, and a package holds no copy of a skill,
so one skill can belong to several packages and a newly authored skill joins
every workspace whose package already matches its name. Names come from two
sources in one flat namespace: the skills this build ships, rendered per
install so they name your real paths and ports, and the ones you author at
`.shared/skills/<name>/SKILL.md`. An authored skill with a shipped skill's name
wins. Opening a workspace renders its selection into `.claude/skills/` and
`.agents/skills/`.

## MCP servers

The shipped catalogue has four entries:

- **`hive-desktop`** is the running app itself: tools to list and create
  profiles, read a profile's graph, read the inbox and feeds, force a source
  refresh, and dry-run a flow against a payload. Nothing to install; it needs
  `http.enabled`, which is on by default. Beta.
- **`hive-canvas`** is the chat's output surface: the agent puts markdown,
  links, and laid-out HTML on named canvases shown in a pane beside the
  conversation. Each canvas is a file under the workspace's `canvases/`, so it
  outlives the chat that made it.
- **`playwright`** gives the agent a real browser through Microsoft's
  `@playwright/mcp`, launched with `npx`. Needs Node and a Playwright browser.
- **`chrome-devtools`** gives the agent DevTools' view of a live Chrome
  through Google's `chrome-devtools-mcp`. Needs Node 20.19 or newer and Chrome.

Your own servers go in `mcps.yaml`:

```yaml
version: 1
servers:
  home-assistant:               # an http or sse server
    title: Home Assistant
    type: http
    url: http://homeassistant.local:8123/mcp
    headers:
      Authorization: "Bearer op://vault/item/token"
  local-tool:                   # a stdio server (type defaults to stdio)
    command: npx
    args: ["-y", "@example/mcp"]
    env:
      TOKEN: "op://vault/item/token"
```

A stdio entry rejects `url` and `headers`; an http or sse entry rejects
`command`, `args`, and `env`.

## Chats and the Code view

A chat can be **pinned** into the Code view's session tree, where it sits
beside your repository sessions and attaches like any other tmux session. That
keeps a long-running agent in reach while you work in a terminal, without
switching areas.
