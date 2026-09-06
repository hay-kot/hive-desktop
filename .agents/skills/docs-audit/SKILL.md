---
name: docs-audit
description: Check the work on this branch (or a PR) for anything a user would need to read about and is not documented, then add or update the page on hivedesktop.com/docs. Use when asked to audit the docs, to decide whether a change needs a docs page, or as a pre-PR pass over a feature that added a setting, a node, an action type, a surface, or a failure mode.
---

# Audit the docs

The docs site is the user's only map of the app, and its failure mode is
silent: a setting ships, a node type lands, a surface is renamed, and the page
that described it keeps describing the old one. Nothing fails a build over it.
This audit is the check.

It covers `web/src/content/docs/` only. `docs/architecture.md`, ADRs, and the
shipped agent skills are other surfaces with their own skills; step 3 says
which change belongs where.

## 1. Scope the change

```bash
git diff main...HEAD --stat
git diff main...HEAD -- internal/app desktop/frontend/src
```

For a PR that is not checked out, `gh pr diff <number>`.

## 2. Inventory what a user can now see, do, or hit

Read the diff for these and write them down before deciding anything:

- **a setting**: a field on `settings.Settings`, a new mock mode, a new
  environment variable read anywhere;
- **a node or action type**: a `connector.Descriptor`, a flow node, an entry
  in `actions.Doc`, a field on any of their configs, a change to a payload
  contract;
- **a surface**: a new view, area, settings section, drawer, dialog, or a
  rename of one the docs refer to by name (`Settings ▸ Integrations`,
  `Chats`, `Code`);
- **a verb or a key**: a command in `keybindings/catalog.ts`, a default combo,
  a launcher rule;
- **an agent-facing thing**: a shipped skill, an MCP catalogue entry, a
  workspace manifest field;
- **a failure a user meets**: a new error message, a new precondition (a
  binary on PATH, a permission, a version floor), a removed fallback.

A refactor, a test, an internal rename, and a change to how something is
drawn all inventory to nothing. Say so and stop. An audit that finds nothing
is a valid outcome and is reported as one.

## 3. Decide where each one belongs

**A user-facing fact goes on the docs site**, on the page that already owns
its subject. The site's structure mirrors the app:

| Subject | Page |
| --- | --- |
| a setting, a path, an environment variable | `configuration/settings.md` |
| a command id or a default combo | `configuration/keybindings.md` |
| a source node, what an account needs, the webhook contract | `concepts/sources.md` |
| a flow node type, wiring, the editor | `concepts/flows.md` |
| an action type, inputs, targets, launchers | `concepts/actions.md` |
| a workspace manifest field, a skill package, an MCP entry | `concepts/agent-workspaces.md` |
| Code: attach, windows, launchers, typography | `concepts/terminal-mode.md` |
| a failure and its way out | `help/troubleshooting.md` |
| first run, the starter feeds | `getting-started/*.md` |
| building the app | `getting-started/build-from-source.md` |

A new page is rare. It is right when a subject has no owner in that table and
would not fit as a section of one, not when a feature is big. Adding one is
the `web-docs` skill's procedure: the file, its frontmatter, and the group.

**These are not docs-site changes**, and saying so is half the audit:

- **How the app is built** goes in `docs/architecture.md`; **why a choice was
  made** goes in an ADR. A user page says what a thing does and what it needs,
  never how it is implemented.
- **A change to a config schema also changes what the agent is told.** The
  shipped skills under `internal/app/prompts/templates/` and the per-type docs
  they splice in (`flow/docs/*.md`, `actions/docs/*.md`, `mcpcatalog/docs/*.md`)
  are the agent's reference. A field documented on the site but not there
  means the Hive workspace writes it wrong. That is the `shipped-skills`
  skill's procedure; run it beside this one.
- **A user-visible change owes a release-notes line** in
  `internal/app/releasenotes/changelog/next.md`. That is the `release-notes`
  skill.
- **Developer-only facts** (mock modes, `launch.env`, the devserver) belong
  on the build page only to the extent a person building the app must know
  them. The rest is `docs/development.md` and `desktop/AGENTS.md`.

## 4. Write it

The `web-docs` skill covers the mechanics: frontmatter, groups, the build.
The conventions that keep the pages consistent:

- **Show configuration as YAML, not as a schema table.** A block with the
  key, its default, and the environment variable in a trailing comment is
  what `settings.md` does throughout, and what the config pages of the `hive`
  CLI's docs do. A table is for a closed set of values or for keys, never
  for a schema.
- **Name things what the app names them.** Settings sections are
  `Settings ▸ Integrations`; the areas are Inbox, Code, and Chats; a
  workspace is a flow. Read `sectionMeta.ts` and the catalog's `group` values
  rather than guessing.
- **Say what a thing needs before what it does.** tmux 3.2, an agent on
  PATH, a scope on a token, a permission: the precondition goes first, in a
  callout if a user would otherwise discover it by failing.
- **Point at the Hive workspace where an agent could do the work.** Every
  config page carries a short callout naming the skill the seeded `Hive`
  workspace has for that file. Add one to a page that gains a config
  surface; do not add one to a page about a thing an agent cannot drive.
- **A callout is `> [!TIP] Title` on the first line of a blockquote**, with
  `NOTE`, `TIP`, `IMPORTANT`, `WARNING`, or `CAUTION`. See
  `web/src/lib/remark-callouts.mjs`.
- **Body content starts at `##`.** The layout renders `title` as the `h1`
  and `description` as the lede.
- **Do not hand-maintain what is derived.** The sidebar, the pager, the
  search index, `llms.txt`, `llms-full.txt`, and each page's `.md` twin are
  all generated from the collection. Adding a page adds it everywhere.

A fact you cannot verify in the code does not go on the page. Read the
struct, the descriptor, or the component; the docs are reviewed as a spec.

## 5. Verify

```bash
cd web && npm ci && npm run build
```

The build validates frontmatter and fails on a bad group by name. Then check
the two things the build cannot:

1. **Every internal link resolves.** Grep the new text for `](/docs/` and
   confirm each target is a page id in `src/content/docs/` (plus an anchor
   that is a real `##` heading, slugified).
2. **The page says what the code does.** Re-read the diff beside the page.
   A default, a range, a scope name, and a key are the four things most often
   wrong.

Nothing in PR CI builds `web/`; the deploy job on `main` is the only other
build. What you skip here surfaces after merge.

## Report

Say what you added or changed, page by page, and say what you decided
**not** to document and why. "This is internal shape and belongs in
architecture.md", "the shipped skill needs the same field", and "no user can
see this" are all conclusions worth writing down, because the next audit
re-asks the same questions.
