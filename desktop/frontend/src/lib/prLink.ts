import type { SessionPullRequest } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

// The two shapes a pull request gets shared in, ported from the `prlink` shell
// script this replaces. Kept byte-identical to it: links already pasted in
// Slack and in chat threads read a certain way, and a link that formats
// differently depending on which of the two produced it is worse than either.
//
// Both formats are offered at the point of copying rather than chosen by a
// setting. Which one you want depends on where you are pasting — Slack takes
// the Markdown link, a text message takes the plain one — so a stored
// preference would just be a trip to Settings before every other paste. This is
// also why the script takes `--plain` as a flag rather than reading a config.

/** The line counts as both formats render them: `(+420, -37)`. */
function counts(pr: SessionPullRequest): string {
  return `(+${pr.additions}, -${pr.deletions})`
}

/**
 * The Slack and Markdown form: one link whose text carries the repository,
 * title and diff size.
 */
export function markdownPullRequestLink(pr: SessionPullRequest, repo: string): string {
  return `[[${repo}] ${pr.title} \`${counts(pr)}\`](${pr.url})`
}

/**
 * The plain-text form for somewhere that will not render Markdown, with the
 * URL on its own line so it stays clickable in a chat client that linkifies.
 */
export function plainPullRequestLink(pr: SessionPullRequest, repo: string): string {
  return `[${repo}] ${pr.title} ${counts(pr)}\n${pr.url}`
}
