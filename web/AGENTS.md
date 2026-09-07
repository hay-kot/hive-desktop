# Agent Instructions — Landing page and docs

Scope: `web/`, the public site at `hivedesktop.com`. The repository-root
`AGENTS.md` still applies (git standards, quality gates).

## What the site is

A Zensical site, the successor of Material for MkDocs by the same team.
`zensical.toml` is a copy of the hive CLI's docs configuration (theme variant,
features, palette, markdown extensions) and holds the nav; the hive CLI's docs
are the reference for look and feel. Pages are Markdown under `docs/`. The
build writes `site/`, which Cloudflare serves as Worker static assets
(`wrangler.jsonc`; the custom domain is declared there and attaches on
deploy). ADR the-site-is-zensical-built-under-mise-served-as-worker-static-assets
records the choice.

`mise.toml` stands on its own: it pins Python, uv, and Node on top of the
repository's toolchain (`mise.lock` beside it holds the resolved versions)
and owns the site's tasks. Run them from inside `web/`:

| Task | What it runs |
| --- | --- |
| `mise run install` | `uv sync --frozen` and `npm ci`; once per fresh worktree |
| `mise run build` | `zensical build --clean --strict`, then `python scripts/llms.py` |
| `mise run dev` | `zensical serve`, live reload on http://127.0.0.1:8000 |
| `mise run preview` | the build, then `wrangler dev` over `site/`, the way production serves it |
| `mise run deploy` | the build, then `wrangler deploy`; CI's and the release tool's job, never by hand |
| `mise run lock` | `uv lock`, after editing `pyproject.toml` |

Zensical is pinned in `pyproject.toml` and resolved into `uv.lock`;
`package.json` carries wrangler only.

## Pages

A page is `docs/<directory>/<slug>.md` plus a line in the nav in
`zensical.toml`. The nav is the site's structure: its order is the tab and
sidebar order, a nested table is a sidebar group, and a page that is not
listed builds without a warning but has no nav entry, no `llms.txt` line, and
no Markdown twin.

Two tabs beside Home. **Getting started** is the reading path:
`getting-started/index.md` (with the `## Install` section), then the sidebar
groups **First run** (`getting-started/sign-in.md`, `notifications.md`,
`first-feed.md`), **Inbox** (`inbox/`), **Code** (`code/`), and **Chats**
(`chats/`), then `getting-started/build-from-source.md` and
`getting-started/troubleshooting.md`. **Configuration** is the reference
tab: `configuration/settings.md` and `configuration/keybindings.md`. The
sidebar groups map one to one onto the app's areas, and that is the rule for
placing a new page: it goes in the directory and group of its area.

Frontmatter is `icon: lucide/<name>` and `description:`. The body starts with
`# Title`, then the description as the lede, then `##` sections. Callouts are
admonitions (`!!! tip "Title"`, body indented four spaces), content tabs are
`=== "macOS"`, keys are `<kbd>`, internal links are relative Markdown-file
links that the strict build validates, and configuration is annotated YAML
rather than a schema table. `docs/getting-started/index.md` and
`docs/configuration/settings.md` are the models.

Two repo skills govern this site: `web-docs` (the mechanics of a page) and
`docs-audit` (whether a change on a branch needs one, and where).

The landing page is `docs/index.md`, HTML sections styled by
`docs/stylesheets/extra.css`. Files under `docs/` that are not Markdown are
copied to the site root unchanged: `install.sh`, `robots.txt`,
`assets/favicon.svg`, and `javascripts/install.js`, which fills the version
span in the `## Install` section.

After the build, `scripts/llms.py` derives `site/llms.txt` (one link per
page), `site/llms-full.txt` (every page inlined), and a Markdown twin of every
nav page at its URL plus `.md` (`/inbox/flows.md` beside `/inbox/flows/`).
`overrides/main.html` adds a `<link rel="alternate" type="text/markdown">`
pointing at `/llms.txt` to every page.

## Worker routes

`worker/index.ts` runs for `/api/*` and for anything the asset router did not
match:

- `/api/latest` — proxies the release manifest for the download CTA.
- `/api/report` — gzipped diagnostic bundles from the app's problem reporter,
  written to the private `hive-desktop-reports` R2 bucket (ADR
  in-app-problem-reporting).
- Old URLs 301-redirect to where their content went. `/docs` to
  `/getting-started/`; `/install` to `/getting-started/#install`; `/compare`
  and `/compare/*` to `/`; the moved pages `/docs/concepts/*` to `/inbox/*`,
  `/code/*`, or `/chats/*`, `/docs/help/troubleshooting` to
  `/getting-started/troubleshooting/`, `/docs/help/reporting-a-problem` to
  `/getting-started/troubleshooting/#report-a-problem`, and
  `/docs/help/updates` to `/configuration/settings/#updates`; any other
  `/docs/<path>` to `/<path>/`. The installed app's About pane still links
  `/docs` and `/docs/help/updates`
  (`desktop/frontend/src/composables/useAboutSettings.ts`), so a page move,
  or a rename of the `## Install`, `## updates`, or `## Report a problem`
  heading, means updating the table.

## CI and deploy

`.github/workflows/ci.yml` runs `mise run install` and `mise run build` in
`web/` on every PR (the `web-build` job), so a broken link or a missing nav
file fails the PR. `.github/workflows/deploy-web.yml` runs the same two tasks
and then `npx wrangler deploy` on a push to `main` that touches `web/**`. The
release tool (`cmd/release/publish.go`) also deploys, with `mise run install`
and `mise run deploy` from `web/`, before it uploads a release.

## Guardrails

- Never deploy. `mise run deploy`, `npm run deploy`, and `npx wrangler deploy`
  all publish to hivedesktop.com.
- `site/` is build output; `.venv/`, `node_modules/`, and `.wrangler/` are
  local state. All are gitignored. Never edit a file under `site/` and never
  commit any of them.
- This is user-facing product documentation only. `docs/architecture.md`,
  ADRs, and in-app copy are not this site.
