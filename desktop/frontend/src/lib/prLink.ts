import type { SessionPullRequest } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

// How a pull request gets shared, ported from the `prlink` shell script this
// replaces and kept byte-identical to it: links already pasted into Slack and
// chat threads read a certain way, and one that formats differently depending
// on which tool produced it is worse than either.
//
// The script also has a `--plain` form. It is not here: the bar shipped both as
// buttons, nobody reached for the plain one, and a second button is width spent
// on a choice not being made. Re-adding it is this function again plus a button.

/** The line counts as the link renders them: `(+420, -37)`. */
function counts(pr: SessionPullRequest): string {
  return `(+${pr.additions}, -${pr.deletions})`
}

/** One link whose text carries the repository, title and diff size. */
export function markdownPullRequestLink(pr: SessionPullRequest, repo: string): string {
  return `[[${repo}] ${pr.title} \`${counts(pr)}\`](${pr.url})`
}
