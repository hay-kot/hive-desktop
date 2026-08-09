# A workspace skill is opt-in from a shared library, not merged into every workspace

- **Status:** accepted
- **Date:** 2026-08-09

## Context

A workspace declares two kinds of capability: the MCP servers its agent can
call and the skills its agent can read. Only MCPs had a management surface.
`mcps.yaml` is a library — declaring a server there enables it nowhere — and a
workspace's `mcps:` list is what enables one, with the merged catalogue
(shipped registry plus user library) rendered in the workspace editor.

Skills worked the other way. `agentws.Generate` scanned `<root>/.shared/skills`
and merged every directory it found into every workspace's generated tree,
with a workspace's own `skills:` list — limited to shipped `hive-*` prompt
slugs — shadowing on a slug collision. The consequence was that a skill was
either in every workspace or in none, there was nothing to read in the app to
find out which skills a workspace carried, and the only skills a user could
name were the ones the build shipped.

## Decision

`.shared/skills/` becomes the skill library: the same directory, read the same
way, but enabling nothing by being present. A skill reaches a workspace only
by having its slug in that workspace's `skills:` list, exactly as an MCP does.

- **One merged catalogue, one shadow rule.** `agentws.SkillCatalogue` merges
  the shipped set (rendered per install by the prompt engine) with the
  library, and a library slug replaces a shipped one of the same name — the
  rule `mcps.yaml` already follows against the shipped MCP registry. The
  shipped half arrives as data (`ShippedSkill`) rather than being read in
  `agentws`, because `internal/app/prompts` imports `agentws` and the
  dependency cannot run the other way.
- **The generator reads no library.** `GenerateInput.Shared` is gone;
  `Generate` installs exactly the skills it is handed. Resolution — library
  file or rendered prompt — happens in the service before `Generate` is
  called, which is what keeps the generator pure over its inputs (spec §4.4).
- **A slug that no longer resolves is reported, not fatal.** `Result`
  gains `MissingSkills` beside `MissingMCPs`. A library skill deleted off disk
  used to fail the whole open; now the workspace opens without it and the UI
  names it. This matters more for skills than for MCPs: a library entry is a
  file a user can move or delete at any time.
- **The editor owns `skills:`.** `WriteManifest` takes a `ManifestEdit` and
  writes both enablement lists, each removed rather than emptied when nothing
  is enabled. Comments and keys the editor does not own still survive, because
  the writer is still the node-tree editor rather than a `yaml.Marshal`.

Authoring a library skill stays a file operation: the editor links to the
directory rather than offering an in-app author flow. A skill is a `SKILL.md`
in a directory named for its slug, which is the format every target agent
already reads, so the library needs no format of its own and installs its
entries verbatim.

## Consequences

- An install whose `.shared/skills/` entries were relied on being everywhere
  loses them until each workspace switches them on. This is a deliberate,
  unmigrated behaviour change: the merge and the enablement list cannot both
  be true, and the scoping is the point of the feature.
- The seeded `hive` workspace still has its `skills:` list rewritten to the
  full shipped set on every startup (`SyncHiveWorkspaceSkills`), so a skill
  toggled off there comes back at the next launch. That workspace exists to
  drive Hive Desktop and tracks the shipped set by design; every other
  workspace keeps whatever the editor wrote.
- A prompt that cannot render without frontend-supplied context (the
  keybindings command catalog) is absent from the catalogue rather than
  offered and unresolvable — the shipped half is rendered against an empty
  `prompts.Input`.
- Nothing about drift changes: the generated skills tree stays disposable and
  reconciled, never hash-tracked (ADR workspace-directories-are-generated-and-disposable).

## Reference

Related decisions: ADR workspace-directories-are-generated-and-disposable (the
generated/authored split the skills tree lives in), ADR
a-workspace-declares-its-own-authority (the capability declaration this is the
other half of), ADR skill-installer (the drift-tracked installer for skills
written into `~/.claude`, which this is deliberately not).
