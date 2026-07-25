# Launch session

A **launch-session** action starts a hive coding session from the triggering
item. It is the action type behind "review this PR" / "start work on this
issue" buttons and behind flow `action` nodes that spawn agents automatically.

## Fields

- `prompt_template` (required) — the new session's initial prompt.
- `repo_template` — which repository the session is created against. Set it and
  the action can run **headlessly** (a flow `action` node can fire it with no
  human present). Leave it empty and the action becomes interactive: the detail
  pane asks for repository, session name, and agent before launching, and a
  flow `action` node is **rejected at validation time** for referencing it.
- `agent` — a non-default agent profile (e.g. `claude`, `aider`). Omit for the
  launcher's default.

## Templates

`prompt_template` and `repo_template` are Go `text/template` strings rendered
over the triggering message (see "Template data" above). A GitHub-shaped item
exposes `{{ .Payload.repo }}`, `{{ .Payload.title }}`, `{{ .Payload.url }}`,
`{{ .Payload.body }}`, `{{ .Payload.num }}`, and `{{ .Payload.author }}`. A
webhook-sourced item exposes whatever its payload carries, so reshape it with a
`function` node first if you want stable names.
