<!--
TITLE — Conventional Commits: type(scope): summary

  feat(chats): pin a chat into the Code view's session tree
  fix(terminal): stop the emulator answering queries on tmux panes
  refactor(flows): fold the node drawer into the editor's own registry
  feat(canvas)!: key a canvas by workspace and name

Imperative, lower-case after the colon, no trailing period, <= 72 chars total.
Append ! before the colon for a breaking change.

  types   feat fix perf refactor docs test build ci chore revert
  scopes  chats canvas terminal code feed flows tasks settings sources
          actions mcp release skills web docs deps repo

A change that genuinely spans the app takes no scope: `refactor: ...`. If the
scope you want is missing, add it here in the same PR rather than inventing one
silently. Renovate titles are exempt — they are the bot's, not ours.

BODY — replace this comment with why the change exists, plus anything a
reviewer cannot get from the diff. The commits carry the reasoning, so this is
shorter than they are. No "Summary" or "Changes" heading, no bullet per file,
no test-passing claim for tests you did not run. Link the issue on its own last
line: Closes #123
-->

<!--
If an LLM writes this pull request title, body, or any commit message on the
branch, use ASD-STE100 Simplified Technical English. Use active voice,
approved-vocabulary words, sentences of 20 words or fewer, and no gerunds.
See CONTRIBUTING.md > Writing style.
-->
