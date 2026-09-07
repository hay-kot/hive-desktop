---
icon: lucide/play
description: The actions.yml schema. Action types, declared inputs, targeting a terminal session or window, and quick-terminal launchers.
---

# Actions

The actions.yml schema. Action types, declared inputs, targeting a terminal session or window, and quick-terminal launchers.

An action is something Hive can do on your behalf: start an agent session on a
PR, run a shell command, publish a message to a topic, or put rendered text on
the clipboard. Actions live in `actions.yml` in your config directory and show
up in three places:

- the **detail pane** and the **…** menu of a selected feed item;
- the row menus of a **terminal session** or **window** in Code;
- a flow's **action node**, which fires the action automatically for every
  item routed there.

**Settings ▸ Actions** edits the same file with a form. The file is watched and
reloaded on save, the schema is strict, and a file that fails to parse leaves
the last good set in place.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace under **Chats** carries the `hive-actions` skill,
    which is this reference with your install's real path. Describe the button
    you want and it writes the entry.

## The file

```yaml
version: 1
actions:
  - id: review-pr                # slug, unique across the file
    label: Review PR             # the button text
    type: launch-session         # one of the four types below
    targets: [item]              # item (default), session, window
    applies_to: [pr]             # item kinds; empty means any
    show_in_detail: true         # offer it as a button in the detail pane
    inputs: []                   # values to ask the user for, see below
    # ...plus the type's own fields, flattened at this level
launchers: []                    # quick terminals, see below
```

`actions:` is in presentation order: every surface that offers actions lists
the applicable ones in the order they appear here.

- `targets` says where an action is offered. Omit it for a feed-item action;
  `session` and `window` put it in a terminal row's menu. A `launch-session`
  action cannot target a terminal, because it creates a *new* session and
  there is no New Session form on those rows.
- `applies_to` refines the `item` target by kind: `pr`, `issue`, or whatever
  `kind` a webhook or command item carries (`Item` when it carries none). It
  does not restrict flow action nodes, which name one action explicitly.
- `show_in_detail` only controls the manual button. A flow can fire an action
  either way.

## Declared inputs

An action can ask for values when it runs. Each input is collected in a small
form before the action fires and read in templates as `{{ .Inputs.<name> }}`.

```yaml
inputs:
  - name: reason                 # a template identifier: letters, digits, underscores
    label: Reason
    type: multiline              # text (default), multiline, or select
    required: true
    placeholder: why this alert is being silenced
  - name: window
    label: For how long
    type: select
    default: 1h                  # must be one of the options
    options: [1h, 24h, 7d]       # required for, and only valid on, select
```

An action with a required input that has no default can only be run by a
person. A flow action node cannot fire it, because there is nobody to ask, and
the flow is rejected at validation time for referencing it.

## Template data

Every `*_template` field is a Go `text/template` rendered over whatever the
action was invoked against. Which values exist depends on the target, and a
template that reads the wrong target's data fails the run rather than
rendering blank.

On the `item` target:

- `{{ .Payload.<field> }}`: the item's payload. GitHub and Gitea items carry
  `repo`, `title`, `url`, `num`, `author`, `body`, `labels`, and `state`.
  Webhook and command items carry whatever was posted or printed.
- `{{ .Key }}`: the item's stable identity, for example `colonyops/hive#2841`.
- `{{ .Raw }}`: the payload as raw JSON.

On the `session` and `window` targets:

- `{{ .Session.Path }}`: the checkout on disk. Also the default working
  directory of a `shell` action run from a terminal row.
- `{{ .Session.Slug }}`: the session's slug, which is also its tmux session name.
- `{{ .Session.Name }}`, `{{ .Session.ID }}`, `{{ .Session.Repo }}`,
  `{{ .Session.Branch }}`: name, hive id, remote, and worktree branch.
- `{{ .Window.ID }}`: the tmux window id, on `window` only. Address it as
  `{{ .Session.Slug }}:{{ .Window.ID }}` in a tmux command.

On every target, `{{ .Inputs.<name> }}` for each declared input. An undeclared
name is a render error.

Pipe every value that reaches a shell through `shq`. It quotes the value so a
title containing spaces, quotes, or `;` cannot break out of its argument.

## Action types

### `launch-session`

Starts a hive coding session from the triggering item. This is what "review
this PR" and "start on this issue" are, and what a flow uses to spawn agents.

```yaml
- id: review-pr
  label: Review PR
  type: launch-session
  show_in_detail: true
  applies_to: [pr]
  agent: claude                            # optional; omit for hive's default
  repo_template: "https://github.com/{{ .Payload.repo }}.git"
  prompt_template: |
    Review pull request {{ .Payload.title }}

    {{ .Payload.url }}
```

`prompt_template` is required. Set `repo_template` and the action can run
**headlessly** from a flow. Leave it empty and the action becomes interactive:
running it opens the New Session form for repository, name, and agent, and a
flow action node referencing it is rejected. Sessions are created through
hive's own spawn rules, so the windows and agent command are the ones your
`~/.config/hive/config.yaml` defines.

### `shell`

Runs a command line through `sh -c`. Reach for it for anything Hive has no
integration with: a CLI, a script, a `curl` to an internal service.

```yaml
- id: open-in-zed
  label: Open in Zed
  type: shell
  targets: [session]
  command_template: 'zed {{ .Session.Path | shq }}'

- id: run-tests
  label: Run tests
  type: shell
  targets: [session]
  timeout: "10m"                           # quoted duration; a bare number is an error
  command_template: 'mise run test'

- id: interrupt
  label: Interrupt agent
  type: shell
  targets: [window]
  command_template: 'tmux send-keys -t {{ printf "%s:%s" .Session.Slug .Window.ID | shq }} C-c'
```

`cwd` defaults to the session's checkout on a terminal target and to Hive's
own directory on an item target; setting it wins either way. `env` adds
variables. An item run is a durable command whose failure keeps bounded
stdout and stderr in the Activity view. A session or window run is deliberately
not durable: it must stay repeatable and must not replay after a restart, so
its failure lands in the jobs list with the tail of stderr.

### `publish-message`

Renders a message and publishes it to one topic, so another agent or process
listening there can pick it up.

```yaml
- id: notify-oncall
  label: Notify oncall
  type: publish-message
  topic: oncall.alerts                     # a constant; no wildcards, no templates
  message_template: "{{ .Payload.title }}: {{ .Payload.url }}"
```

The topic must be a literal. Routing is an authoring decision, never computed
from payload data.

### `clipboard`

Renders a template and puts the result on the clipboard. "Give me a
ready-to-paste command" without shelling out to `pbcopy`.

```yaml
- id: copy-checkout
  label: Copy checkout command
  type: clipboard
  applies_to: [pr]
  text_template: "gh pr checkout {{ .Payload.num }} -R {{ .Payload.repo }}"
```

A clipboard action is offered wherever it declares a target but is not
runnable from a flow, which has no clipboard to write to.

## Launchers

A launcher opens the pop-up terminal straight into a program: `lazygit` in the
checkout you are looking at, `btop`, a test watcher. It is deliberately not an
action and carries none of the action envelope.

```yaml
launchers:
  - id: lazygit
    label: lazygit
    icon: git-branch                       # terminal (default), git-branch, folder, bug, zap, ...
    command: lazygit
  - id: dotfiles
    label: Edit dotfiles
    icon: folder
    cwd: ~/.dotfiles
    command: $EDITOR .
```

`command` and `cwd` are used exactly as written, not templated. The command
runs through a login shell in the working directory, so your PATH, aliases,
and functions resolve.

`cwd` decides a launcher's reach. Without one, a launcher follows the terminal
you are looking at, opening in its active pane's current directory, and is
offered only while a terminal session is open. With one, it is pinned to that
directory and reachable from anywhere. Every launcher appears in the command
palette and is bindable as `launcher.<id>`:

```yaml
# settings.yaml
keybindings:
  launcher.lazygit: [alt+g]
```

One pop-up is open at a time. Invoking a different launcher replaces what the
panel holds; invoking the one already on screen hides it. Quitting the program
takes the panel down, and a pop-up dies with the app. Anything that has to
survive a restart is a hive session, not a pop-up.

**Settings ▸ Quick terminals** edits the same list.

## A complete file

```yaml
version: 1
actions:
  - id: review-pr
    label: Review PR
    type: launch-session
    show_in_detail: true
    applies_to: [pr]
    repo_template: "https://github.com/{{ .Payload.repo }}.git"
    prompt_template: |
      Review pull request {{ .Payload.title }}

      {{ .Payload.url }}
  - id: start-implementation
    label: Start implementation
    type: launch-session
    show_in_detail: true
    applies_to: [issue]
    agent: claude
    repo_template: "https://github.com/{{ .Payload.repo }}.git"
    prompt_template: |
      Work on {{ .Payload.title }}

      {{ .Payload.url }}

      {{ .Payload.body }}
  - id: open-session-in-zed
    label: Open in Zed
    type: shell
    targets: [session]
    command_template: 'zed {{ .Session.Path | shq }}'
  - id: notify-oncall
    label: Notify oncall
    type: publish-message
    show_in_detail: false
    topic: oncall.alerts
    message_template: "{{ .Payload.title }}: {{ .Payload.url }}"
  - id: silence-alert
    label: Silence alert
    type: clipboard
    show_in_detail: true
    inputs:
      - name: reason
        label: Reason
        type: multiline
        required: true
      - name: window
        label: For how long
        type: select
        default: 1h
        options: [1h, 24h, 7d]
    text_template: |
      amtool silence add alertname={{ .Payload.title | shq }} \
        --duration {{ .Inputs.window }} --comment {{ .Inputs.reason | shq }}
launchers:
  - id: lazygit
    label: lazygit
    icon: git-branch
    command: lazygit
```
