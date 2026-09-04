---
name: release-notes
description: Write the release-notes line a branch owes to internal/app/releasenotes/changelog/next.md. Use when asked to update the changelog or release notes, or to check whether the work on this branch earns an entry before the PR goes up.
---

# Update the release notes draft

A user-visible change appends its line to
`internal/app/releasenotes/changelog/next.md` **in the pull request that earns
it** (`desktop/AGENTS.md`). The draft is what every dev and beta build embeds
and shows, and a stable release ships its bytes unchanged after
`mise run changelog:promote` -- so what you write here is the product's
changelog, not a note to a future maintainer.

**Only ever edit `next.md`.** A `changelog/<version>.md` is written by
promotion during a release, on `main`, by `cmd/release`. Writing one by hand on
a feature branch is wrong even when the version looks obvious, and a file
naming a prerelease is rejected at parse time
(ADR [release-notes-ship-inside-the-binary](../../../docs/decisions/2026-08-06-release-notes-ship-inside-the-binary.md)).

## 1. Find what the branch changed

```bash
git diff main...HEAD --stat
git log main..HEAD --format='%s%n%n%b'
```

The commit bodies carry the intent, which is most of the line already. For a
PR that is not the checked-out branch, `gh pr diff <number>` and
`gh pr view <number>`.

## 2. Decide whether it earns a line

The test is whether a user could notice without reading the diff.

Earns a line:

- a capability that did not exist -- a view, an overlay, a connector, a source,
  a node type, an MCP tool, a palette scope, a shortcut;
- behaviour that changed under someone who was already using it, including a
  default, a keybinding, or where something opens;
- a bug a user could hit, described as the symptom they saw;
- a new `settings.yaml` key or `actions.yml` field they can write.

Earns nothing:

- a refactor with no behaviour change, however large;
- tests, fixtures, mocks, e2e specs;
- CI, mise tasks, lefthook, `cmd/` development tooling;
- `docs/`, ADRs, `AGENTS.md`, agent skills;
- a `hivecore` vendor sync or a dependency bump that changes nothing visible;
- a fix to something that never reached a build a user runs -- if the bug was
  introduced and fixed inside the same draft cycle, correct or delete the line
  that described it instead of adding a "Fixed" entry beneath it.

When in doubt, look at what is already in the file: it is consolidated per
capability, not per pull request.

## 3. Place it

The draft uses `## Added`, `## Changed`, `## Fixed`, in that order. Read the
file first and use the headings it has; add a missing one in that order rather
than inventing a fourth.

**Prefer extending an existing bullet to adding a near-duplicate.** A feature
that landed over five PRs is one entry describing what the app now does, not
five entries describing five days of work. If a bullet already covers the
surface you touched, rewrite it to include the new behaviour.

If the branch reverts or removes something the draft describes, edit that
bullet out. Promotion moves the bytes unchanged, so a stale draft line ships as
a stable release note.

## 4. Write it

This is product copy a user reads inside the app. **The Simplified Technical
English rule in `CONTRIBUTING.md` applies to commit and PR text, not here** --
write these the way the surrounding entries are written.

Follow the shape already in the file:

- open with the thing in bold, then say what it does and where it is:
  `**A notify terminal node**, so a feed can notify on new items.`
- present tense, addressed to the user, describing the app rather than the
  work: "the palette knows where you are", never "we added" or "this PR";
- name the surface a user can find (`Settings ▸ Terminal`, the command palette,
  the Code view's session tree) and config keys as they are written
  (`profiles.order` in `settings.yaml`);
- for a `Changed` entry, say what a user has to do differently, and for a
  `Fixed` entry, describe the symptom, not the cause;
- mark an unfinished area `(experimental)` the way Terminal mode and Chats are;
- never name a Go package, a Vue component, an ADR, an issue, or a PR number.

## 5. The `summary` frontmatter

One sentence, and the only frontmatter key the draft carries. It is what the
What's New toast shows and what a channel manifest carries for a release the
user has not installed yet, so it describes the **whole accumulated draft**,
not your line. Leave it alone unless the branch adds something big enough to
change the release's headline; if you do rewrite it, keep it one sentence.

Never add `version:` or `date:` to the draft. Promotion stamps both.

## 6. Verify

```bash
go test ./internal/app/releasenotes/...
```

`TestChangelogParses` reads the embedded file, so a broken header, a stray
frontmatter key, or an unterminated `---` fails there rather than at release
time. It runs inside `mise run check`, which the pre-push hook already runs.
