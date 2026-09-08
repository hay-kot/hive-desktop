---
name: web-docs
description: Add, edit, or restructure a page on the public documentation site at hivedesktop.com, the Zensical site under web/docs whose nav lives in web/zensical.toml. Use only for user-facing product docs; docs/architecture.md, ADRs, and in-app copy are not this site.
compatibility: Requires mise. web/mise.toml pins Python, uv, and Node and owns the site's tasks; run `mise run install` from inside web/ once in a fresh worktree (web/.venv and web/node_modules are gitignored). PR CI builds the site in the `web-build` job, so a broken link fails the PR.
---

# Update the docs site

The site is Zensical, the successor of Material for MkDocs by the same team,
configured in `zensical.toml` as a copy of the hive CLI's docs configuration
(ADR the-site-is-zensical-built-under-mise-served-as-worker-static-assets).
A page is a Markdown file under `docs/` plus one line in the nav. Every path
below is relative to `web/`, and every `mise run` below runs from inside it.

To decide *whether* a change needs a page, and which one, use the
`docs-audit` skill; this one is the mechanics.

## Where a page lives

`docs/<directory>/<slug>.md`. The file's path is its URL: `inbox/flows.md` is
`/inbox/flows/`, and a directory's `index.md` is its root
(`getting-started/index.md` is `/getting-started/`). There is no `/docs`
prefix any more; the Worker redirects the old URLs (see Guardrails).

The nav has two tabs beside Home (`index.md`, the landing page):

- **Getting Started** is the reading path. `getting-started/index.md` (with
  the `## Install` section) comes first, followed by five sidebar groups.
  **First run** contains `getting-started/sign-in.md`, `notifications.md`, and
  `first-feed.md`. The app areas each have a group and directory: **Inbox**
  (`inbox/how-it-works.md`, `flows.md`, `sources.md`, `actions.md`),
  **Code** (`code/terminal-mode.md`), and **Chats**
  (`chats/agent-workspaces.md`). **Resources** contains
  `getting-started/build-from-source.md` and `troubleshooting.md`.
- **Configuration** is the reference tab: `configuration/settings.md` and
  `configuration/keybindings.md`.

Inbox, Code, and Chats each map to one app area. A page about something the
user does in Inbox goes in `inbox/` and the Inbox group. Build and support
pages go in Resources. A setting or key goes on its Configuration page.

## The nav is hand-maintained

`zensical.toml` holds the nav, and the nav is the site's structure. Its order
is the tab order and the sidebar order, a nested table is a sidebar group,
and a nav entry is a bare path, so the page names itself with its `# Title`.
Adding a page is two edits: the file and its line in the nav.

A page that is not in the nav still builds and is reachable by URL, and the
strict build does not warn about it. It has no tab or sidebar entry, no line
in `llms.txt`, and no Markdown twin, so nobody finds it. Check the nav
whenever you add a file.

## Frontmatter and body

```yaml
---
icon: lucide/settings   # shown beside the title in the nav
description: Change Hive Desktop preferences in the app or through settings.yaml.
---

# Settings

Open Settings with <kbd>⌘,</kbd> or from the command palette.

## Configuration files
```

- `icon` is a `lucide/<name>` icon.
- The body starts with `# Title`, followed by a short task-oriented lede when
  the title needs context. Do not repeat the frontmatter description.
- `description` becomes the page's line in `llms.txt`
  (`scripts/llms.py` reads the frontmatter).

`docs/getting-started/index.md` and `docs/inbox/sources.md` are useful models.

## Writing conventions

- **Keep it short.** Include what a user needs to complete a task, avoid data
  loss, meet a requirement, or recover from a failure. Remove implementation
  detail and explanations of ordinary controls.
- **State facts directly.** Avoid em and en dashes, rhetorical contrasts such
  as "not X, but Y", "Why it matters" headings, staged reveals, and marketing
  filler.
- **Do not duplicate an owner page.** Link to the page that owns a subject.
  Sources owns provider support; Settings owns configuration locations; the
  shortcut dialog owns the complete live shortcut list.
- **Summarize visible options.** Use a sentence or bullets for groups such as
  fonts, terminal spacing, notification delivery, and update channels. Do not
  document every visible setting or explain why someone might change it.
- **Use exact configuration only when needed.** A short YAML example is useful
  for manual-only settings and file formats. Do not reproduce a whole schema
  already available in the app or a shipped skill.
- **Callouts are admonitions.** `!!! tip "Title"` on its own line, body
  indented four spaces. The pages use `tip`, `note`, and `info`; `???` in
  place of `!!!` makes one collapsible (`pymdownx.details`). Use one for a
  precondition a user would otherwise discover by failing, and for the Hive
  workspace pointer on a config page.
- **Content tabs** are `=== "macOS"` / `=== "Linux"` blocks, body indented
  four spaces (`pymdownx.tabbed`). The `## Install` section of
  `getting-started/index.md` is the example.
- **Point config pages at the Hive workspace.** The app seeds a Chats
  workspace named `Hive` carrying every shipped `hive-*` skill. A page about
  a file the agent can edit carries a short `!!! tip "Ask the Hive workspace"`
  naming that skill (`hive-settings`, `hive-flows`, `hive-actions`, ...).
- **Name things what the app names them.** `Settings ▸ Integrations`, the
  Inbox / Code / Chats areas, workspace, feed. Check
  `desktop/frontend/src/components/settings/sectionMeta.ts` and
  `desktop/frontend/src/keybindings/catalog.ts` before writing a label.
- **Keys are `<kbd>` elements**: press `<kbd>g</kbd>` then `<kbd>a</kbd>`.
- **Internal links are relative Markdown-file links**, with an anchor when
  one is needed: `../configuration/settings.md#updates`, `sign-in.md`. The
  strict build validates them, and `scripts/llms.py` rewrites them to
  absolute URLs in the Markdown twins. A root-relative link (`/llms.txt`) is
  for a file at the site root, not for a page.
- Mermaid fences, `attr_list`, `md_in_html`, and emoji shortcodes are
  enabled in `zensical.toml`; the hive CLI's docs show when each is worth
  using.

## What is derived and what is not

The build derives these from the nav and the pages; never hand-edit them:

- the tabs, the sidebar and its groups, the prev/next footer, and the
  per-page table of contents;
- search (`site/search.json`) and `site/sitemap.xml`;
- `site/llms.txt`, `site/llms-full.txt`, and a Markdown twin of every nav
  page at its URL plus `.md` (`/inbox/flows.md` beside `/inbox/flows/`,
  `/getting-started.md` beside `/getting-started/`). `scripts/llms.py` writes
  them after `zensical build`, skipping the landing page because it is HTML.
  `overrides/main.html` adds a `<link rel="alternate" type="text/markdown">`
  pointing at `/llms.txt` to every page's head.

These are hand-maintained:

- the nav in `zensical.toml`;
- the landing page, `docs/index.md`: HTML sections styled by
  `docs/stylesheets/extra.css`, the same shape as the hive CLI's landing
  page. A hero, a strip linking to the three showcase sections (Feeds, Code,
  Chats), each pairing copy with a demo video, and a CTA. There is no
  component model; a change is an edit to those two files;
- files under `docs/` that are not Markdown. The build copies them to the
  site root unchanged: `install.sh`, `robots.txt`, `assets/favicon.svg`, the
  demo videos under `assets/demos/` (`feeds.mp4`, `code.mp4`, `chats.mp4`),
  `javascripts/download.js`, which fills the hero and CTA download buttons
  and the `## Install` section's download panel from `/api/latest`, and
  `javascripts/demos.js`, which swaps a demo player for its "coming soon"
  placeholder when its video file is missing.

## Validate

```bash
cd web
mise run install     # first time in a fresh worktree
mise run build       # zensical build --clean --strict, then scripts/llms.py
mise run dev         # whole site incl. /api/* on http://127.0.0.1:8788
mise run dev:pages   # pages only, browser live reload on http://127.0.0.1:8000
```

`web/mise.toml` stands on its own, so the tasks run from inside `web/`; there
is no repository-root form.

Check anything that reads `/api/*` on `mise run dev`, not `dev:pages`.
`zensical serve` has no way to answer those routes and the release bucket
sends no CORS headers, so on 8000 the download buttons and the `## Install`
panel sit in their fallback state and read as missing.

`--strict` aborts the build on any warning. A link to a page that does not
exist, a link to an anchor that is not a heading on its target, and a nav
entry whose file is missing all report `page does not exist` with a
file:line:column. Strict does not catch a page absent from the nav (see
above) or a fact that is wrong, so re-read the diff beside the page.

`mise run preview` builds and then serves `site/` through the Worker with
`wrangler dev`, the way production does. Use it for a change to the Worker
or a redirect, not for a page edit.

PR CI runs the same build (`web-build` in `.github/workflows/ci.yml`), so a
broken link fails the PR rather than the deploy.

## Guardrails

- **Never deploy.** `mise run deploy`, `npm run deploy`, and
  `npx wrangler deploy` publish to hivedesktop.com; deployment is
  `.github/workflows/deploy-web.yml`'s job on `main`, and the release tool's
  (`cmd/release/publish.go`, the `release` skill).
- `site/` is build output; `.venv/`, `node_modules/`, and `.wrangler/` are
  local state. All four are gitignored. Never edit a file under `site/` and
  never commit any of them.
- These pages are product documentation for users. Internal shape belongs in
  `docs/architecture.md`, and a decision belongs in an ADR under
  `docs/decisions/`.
- Old URLs are redirected by the Worker (`worker/index.ts`), with 301s, and
  two of them are load-bearing: the installed app's About pane links
  `https://hivedesktop.com/docs` and `https://hivedesktop.com/docs/help/updates`
  (`desktop/frontend/src/composables/useAboutSettings.ts`). The table:
  `/docs` to `/getting-started/`; `/install` to `/getting-started/#install`;
  `/compare` and `/compare/*` to `/`; the moved pages `/docs/concepts/*` to
  `/inbox/*`, `/code/*`, or `/chats/*`, `/docs/help/troubleshooting` to
  `/getting-started/troubleshooting/`, `/docs/help/reporting-a-problem` to
  `/getting-started/troubleshooting/#report-a-problem`, and
  `/docs/help/updates` to `/configuration/settings/#updates`; any other
  `/docs/<path>` to `/<path>/`. Moving or renaming a page, or the `## Install`,
  `## updates`, or `## Report a problem` heading, means updating that table.
- Zensical is pinned in `pyproject.toml` and resolved into `uv.lock`. A
  version bump is an edit to `pyproject.toml` followed by `mise run lock`.
