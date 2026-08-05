---
name: shipped-skills
description: Change the agent skills Hive Desktop installs into a user's ~/.claude, ~/.codex, ~/.pi, and ~/.agents — the hive-flows, hive-actions, hive-settings, hive-keybindings, hive-webhook-sources, hive-mcp SKILL.md files. Use when editing what one of those says, adding a new one, or adding a target agent directory.
compatibility: Requires Go and mise. Editing a template changes generated output — never edit an installed SKILL.md under a user's home directory.
---

# Change a shipped skill

A shipped skill is a **rendered prompt**, not a file. `internal/app/prompts`
owns the text (ADR go-owned-llm-prompts) and `internal/app/skills` installs it as a `SKILL.md`
into the directories coding agents scan, keeping it in sync by content hash
(ADR skill-installer). `app.SkillsService` composes the two: slug is `hive-<prompt id>`
and the frontmatter description is the prompt's description plus
`Use when configuring this in Hive Desktop (<target>).`

The `.agents/skills/` files in *this repo* are the project's own dev skills and
have nothing to do with these — do not confuse the two.

## Edit what a skill says

Prompt text lives in `internal/app/prompts/templates/*.tmpl`, never in a Go
string literal or a frontend component. Three rules decide which file to open:

- Wording shared by more than one prompt goes in `fragments.tmpl` and is
  composed with `{{template}}` — write a change once.
- What a prompt says about a **type** comes from that type's own documentation
  — `flow.NodeDoc` for node types, `actions.Doc` for action types, spliced in
  by the `section` function. Fix the type's doc; do not restate it in the
  template.
- Install-specific paths and URLs come from `.Env` (`FlowsDir`, `ActionsPath`,
  `SettingsPath`, `WebhookBaseURL`, `APIBaseURL`, and the `*Enabled` flags), so
  a rendered skill names the real file on that machine instead of a
  placeholder.

## Add a skill

A template in `templates/` plus one entry in `definitions`
(`internal/app/prompts/prompts.go`) with `listed: true`. Nothing else changes:
the installer installs whatever the catalog reports, and the Skills settings tab
lists it with no frontend edit.

`listed: false` marks a context-scoped prompt that needs instance data only its
own editor has (`webhook-transform`) — it is rendered on demand and never
installed as a skill.

The installer enforces the Agent Skills naming rules (`ValidateSkill`): the
slug is 1–64 chars of lowercase letters, digits, and single hyphens, must not
contain a reserved word, and the description caps at 1024 characters — which the
`hive-` prefix and the appended target sentence must fit inside.

## Add a target agent

One entry in `targets` (`internal/app/skills/targets.go`): id, label, default
directory, and the path/body template pair. All four current agents share
`skill.tmpl`; only an agent whose file shape differs needs its own template.
This is a documented extension point — see the extension-point table in
`docs/architecture.md`.

## Validate

```bash
go test ./internal/app/...       # prompts, skills installer, and SkillsService
mise run check                   # the pre-push gate: generate, tidy, lint, test
```

`internal/app/prompts/prompts_test.go` is the real specification and asserts
against the registries, not fixed lists: every definition renders with no
unresolved template field, the flows and actions prompts cover every registered
node/action type, each prompt carries this install's paths, and rendering is
deterministic. Adding a node or action type without extending its doc fails
there.

## Guardrails

- **An installed `SKILL.md` is generated output.** Editing
  `~/.claude/skills/hive-*/SKILL.md` (or the `~/.codex`, `~/.pi`, `~/.agents`
  copies) breaks the content hash, so the app reads it as a user edit and stops
  updating it. Change the template.
- **Do not write install code paths for a new skill.** If a change needs a
  branch in the installer or the settings tab, the skill is being modelled
  wrong — it should be a template plus a registry entry.
- The keybindings prompt requires the frontend command catalog
  (`desktop/frontend/src/keybindings/catalog.ts`) and errors without it; the
  headless on-start sync therefore leaves `hive-keybindings` untouched. That is
  deliberate — do not make it render an empty command table.
- A change to what these prompts say ships to users' home directories on their
  next launch. Keep them accurate about the current schema.
