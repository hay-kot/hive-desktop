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
- `post_hook` — a shell command to run once the session exists, in its
  checkout. See below.
- `post_hook_timeout` — how long the hook may run, as a duration string
  (`"2m"`). Defaults to one minute.

## Post hook

`post_hook` runs after the session is created, through `sh -c`, with the new
session's checkout as the working directory and the login shell's environment
(so `gh`, `zed`, and the rest of your `PATH` resolve). It is the place to put
the setup the agent's prompt cannot do — check out the pull request the item is
about, then open an editor on it:

```yaml
- id: review-pr
  label: Review PR
  type: launch-session
  applies_to: [pr]
  repo_template: "https://github.com/{{ .Payload.repo }}.git"
  prompt_template: "Review pull request #{{ .Payload.num }}"
  post_hook: "gh pr checkout {{ .Payload.num }} && zed ."
```

The hook is rendered over the same data as the other templates, plus
`.Session`, bound to the session that was just created: `{{ .Session.Path }}`,
`{{ .Session.Slug }}` (its tmux session name), `{{ .Session.Name }}`,
`{{ .Session.ID }}`, and `{{ .Session.Repo }}`. `{{ .Session.Branch }}` is
empty here — a fresh session has no branch to report, and the hook is a shell
in the checkout already.

A hook that fails does **not** fail the action: the session exists by then, so
reporting the launch as failed would be untrue and would invite a retry that
creates a second session. Its exit status and output land in the action's run
log instead, under the item it ran for.

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
