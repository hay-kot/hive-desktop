---
icon: lucide/play
description: Configure reusable actions and quick terminal launchers.
---

# Actions

Actions are reusable operations defined in `actions.yml`. Configure them under **Settings ▸ Actions** or edit the file directly.

Actions can appear on feed items, terminal sessions, terminal windows, or in a flow.

## Action types

- **`launch-session`** starts a coding agent session from an item.
- **`shell`** runs a shell command.
- **`publish-message`** sends a message to a topic.
- **`clipboard`** renders text and copies it.

A manual item action can limit itself to kinds such as pull requests or issues. An action can also ask for text or a selected value before running.

## Example

```yaml
version: 1
actions:
  - id: review-pr
    label: Review PR
    type: launch-session
    applies_to: [pr]
    show_in_detail: true
    repo_template: "https://github.com/{{ .Payload.repo }}.git"
    prompt_template: |
      Review pull request {{ .Payload.title }}

      {{ .Payload.url }}
launchers: []
```

This action appears for pull requests and starts a session with the repository and prompt rendered from the item.

Template fields depend on where the action runs:

- Item actions use `{{ .Payload.<field> }}` and `{{ .Key }}`.
- Session actions use values such as `{{ .Session.Path }}` and `{{ .Session.Branch }}`.
- Window actions can also use `{{ .Window.ID }}`.
- Declared inputs use `{{ .Inputs.<name> }}`.

Use the `shq` template function when inserting item or input data into a shell command.

## Where actions run

Use `targets` to choose where an action appears:

```yaml
- id: run-tests
  label: Run tests
  type: shell
  targets: [session]
  timeout: "10m"
  command_template: "mise run test"
```

`item` is the default target. `session` and `window` add the action to row menus in Code. A `launch-session` action only supports item targets.

A flow runs its named action for every routed item, regardless of `applies_to`. Flow actions cannot use the clipboard. A `launch-session` flow action needs `repo_template`, and required inputs need defaults.

## Quick terminals

Quick terminals open a tool in the pop-up terminal. Configure them under **Settings ▸ Quick terminals** or in the `launchers` list:

```yaml
launchers:
  - id: lazygit
    label: lazygit
    command: lazygit
```

A launcher without `cwd` uses the active terminal directory. A launcher with `cwd` is available from anywhere. Launchers also appear in the command palette and can have keyboard shortcuts.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace in **Chats** can update `actions.yml` with the `hive-actions` skill.
