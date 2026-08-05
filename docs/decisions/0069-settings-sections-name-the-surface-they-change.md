# 0069 — Settings sections name the surface they change, and shared values live in General

- **Status:** accepted
- **Date:** 2026-08-04

## Context

Application settings were sectioned by kind — Appearance, System, Automation —
which stopped answering "what does this control change?" once the app grew more
than one surface. Appearance carried the terminal typography that the Code tab,
the pop-up panel and the Agents area's chats all draw with; System carried the
editor command that open-in-editor actions launch, along with storage
locations, diagnostics, an Experimental list and an About card. A control's
placement recorded which pane happened to exist when it shipped.

Two forces made that worse rather than merely untidy. The editor command is
about to have consumers across the Inbox, the Code tab and the Agents area
(#224, #225), so filing it under whichever surface shipped first is now wrong
in a way a reader can observe. And every ships-dark opt-in landed in one
"Experimental" list, which grouped features by how finished they are instead of
by what they do — a category that empties itself as features graduate.

## Decision

Application settings are cut by **the surface a value changes**:

- A value only one surface uses lives on that surface's own pane, named for the
  surface: **Terminal** (typography, behaviour) and **Agents** (workspace root).
- A value several surfaces use lives in **General**. The editor command is the
  first: it is shared rather than agents-owned, so it gets one clearly-named
  home instead of being surfaced in three places.
- **System** is this install on this machine — storage locations, diagnostics,
  and the problem reporter that bundles them. Not a junk drawer for anything
  with a path: the agent-workspace root is a location, and it lives on Agents.
- **About** is its own pane, because the running build is not a setting. The
  auto-update controls sit with it, where "what am I running" is answered.
- **Experimental is a posture, not a category.** A ships-dark opt-in (ADR 0037)
  renders on the pane for the feature it gates, carrying an Experimental badge
  and its restart-pending state (`settings/ExperimentalToggle.vue`). There is
  no Experimental section to graduate out of.

`/settings/system` keeps working: System survives the re-cut as a real section
with a narrower charter, so no route is retired and no bookmark breaks.

The YAML is untouched. `editor` was already a top-level key in `settings.yaml`,
so this is a UI-placement change and needs no forward-only migration (ADR 0032).

## Consequences

- A pane's name is a claim about scope, and reviews can check it: a control
  whose effect is not what the pane is named for belongs elsewhere. "Which pane
  does this go on?" has an answer that does not depend on shipping order.
- Adding or renaming a section stays three coordinated edits —
  `applicationSettingsSections` in `router.ts`, `categoryMeta` and `navGroups`
  in `SettingsView.vue` — and `SettingsView.spec` still fails a partial one.
- Error and empty-state copy names a pane, so moving one is a code change with
  a search cost. That is the price of wayfinding that is true; the strings are
  few and a stale one is a bug, not a cosmetic drift.
- A surface with a single setting still gets a pane (Agents is one opt-in and
  one path today). A pane that would hold nothing does not get created — the
  Inbox has no settings of its own and has no section.
- The editor command is a combobox rather than a list of the four detected
  launchers, so any single-word command can be set without hand-editing
  `settings.yaml`. It persists on a debounce, since every keystroke emits and
  each write is a read-modify-write of the file.
