# Launch session

A **launch-session** action starts a hive coding session from the triggering
item. It is the action type behind manually invoking "review this PR" /
"start work on this issue" on an item, and behind flow `action` nodes that
spawn agents automatically.

## Fields

- `prompt_template` (required) — the new session's initial prompt.
- `repo_template` — which repository the session is created against. Set it and
  the action can run **headlessly** (a flow `action` node can fire it with no
  human present). Leave it empty and the action becomes interactive: invoking
  it manually prompts for repository, session name, and agent before the
  session launches, and a flow `action` node is **rejected at validation
  time** for referencing it.
- `agent` — a non-default agent profile (e.g. `claude`, `aider`). Omit for the
  launcher's default.

## Item target only

A launch-session action creates a *new* session, so it cannot declare
`targets: [session]` or `targets: [window]` — the terminal's row menus have no
New Session form to collect the interactive variant's repository and name, and
the headless variant's `repo_template` renders over a feed item's payload,
which a terminal target carries none of. Declaring one is rejected when the
catalog is parsed.

## Templates

`prompt_template` and `repo_template` are Go `text/template` strings rendered
over the triggering message (see "Template data" above). A GitHub-shaped item
exposes `{{ .Payload.repo }}`, `{{ .Payload.title }}`, `{{ .Payload.url }}`,
`{{ .Payload.body }}`, `{{ .Payload.num }}`, and `{{ .Payload.author }}`. A
webhook-sourced item exposes whatever its payload carries, so reshape it with a
`function` node first if you want stable names.
