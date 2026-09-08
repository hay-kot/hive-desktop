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
| `mise run dev` | `zensical serve` behind `wrangler dev`: the whole site, Worker routes included, on http://127.0.0.1:8788 |
| `mise run dev:pages` | `zensical serve` alone, browser live reload on http://127.0.0.1:8000; `/api/*` is absent |
| `mise run preview` | the build, then `wrangler dev` over `site/`, the way production serves it |
| `mise run deploy` | the build, then `wrangler deploy`; CI's and the release tool's job, never by hand |
| `mise run lock` | `uv lock`, after editing `pyproject.toml` |

Zensical is pinned in `pyproject.toml` and resolved into `uv.lock`;
`package.json` carries wrangler only.

**Use `mise run dev` for anything that touches `/api/*`.** `zensical serve`
cannot answer those routes and `dl.hivedesktop.com` sends no CORS headers, so
on the pages-only server the download buttons and panel stay in their
fallback state and look broken. `dev` keeps `zensical serve` rebuilding
`site/` on save and puts `wrangler dev` in front of it, so a save still shows
up on 8788 (refresh by hand; the live-reload socket belongs to 8000).

## Pages

A page is `docs/<directory>/<slug>.md` plus a line in the nav in
`zensical.toml`. The nav is the site's structure: its order is the tab and
sidebar order, a nested table is a sidebar group, and a page that is not
listed builds without a warning but has no nav entry, no `llms.txt` line, and
no Markdown twin.

Two tabs beside Home. **Getting Started** is the reading path:
`getting-started/index.md` (with the `## Install` section), then the sidebar
groups **First run** (`getting-started/sign-in.md`, `notifications.md`,
`first-feed.md`), **Inbox** (`inbox/`), **Code** (`code/`), **Chats**
(`chats/`), and **Resources** (`getting-started/build-from-source.md`,
`troubleshooting.md`). **Configuration** is the reference tab:
`configuration/settings.md` and `configuration/keybindings.md`. App pages go
in the group for their area. Build and support pages go under Resources.

Frontmatter is `icon: lucide/<name>` and `description:`. The body starts with
`# Title`; do not repeat the frontmatter description below it. Callouts are
admonitions (`!!! tip "Title"`, body indented four spaces), content tabs are
`=== "macOS"`, keys are `<kbd>`, and internal links are relative
Markdown-file links that the strict build validates.

Keep pages short and task-focused. State facts directly. Avoid em and en
dashes, rhetorical contrasts, "Why it matters" headings, staged reveals, and
marketing filler. Do not explain every visible control. Summarize ordinary
settings and shortcuts by category, and use exact YAML only when a user needs
to edit it manually. Keep each fact on the page that owns it and link there
instead of repeating it.

Two repo skills govern this site: `web-docs` (the mechanics of a page) and
`docs-audit` (whether a change on a branch needs one, and where).

The landing page is `docs/index.md`, HTML sections styled by
`docs/stylesheets/extra.css`: a hero, a strip linking to the three showcase
sections (Feeds, Code, Chats), each pairing copy with a demo video, and a
CTA. Files under `docs/` that are not Markdown are copied to the site root
unchanged: `install.sh`, `robots.txt`, `assets/favicon.svg`, the demo videos
under `assets/demos/` (`feeds.mp4`, `code.mp4`, `chats.mp4`),
`javascripts/download.js`, which fills the hero and CTA download buttons and
the `## Install` section's download panel from `/api/latest`, and
`javascripts/demos.js`, which swaps a demo player for its "coming soon"
placeholder when its video file is missing.

After the build, `scripts/llms.py` derives `site/llms.txt` (one link per
page), `site/llms-full.txt` (every page inlined), and a Markdown twin of every
nav page at its URL plus `.md` (`/inbox/flows.md` beside `/inbox/flows/`).
`overrides/main.html` adds a `<link rel="alternate" type="text/markdown">`
pointing at `/llms.txt` to every page.

## Worker routes

`worker/index.ts` runs for `/api/*` and for anything the asset router did not
match:

- `/api/latest` — proxies a release channel manifest for the download buttons.
  `?channel=stable|beta|dev`, stable by default. `download.js` asks for `dev`
  because no stable manifest is published yet.
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
  or a rename of the `## Install`, `## Updates`, or `## Report a problem`
  heading, means updating the table (heading ids are lower-cased, so
  `## Updates` still answers `#updates`).

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
