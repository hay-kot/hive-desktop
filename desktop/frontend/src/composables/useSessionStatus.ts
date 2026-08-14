import { ref, watch, onScopeDispose, type Ref } from 'vue'
import {
  SessionGitStatus as ReadGitStatus,
  SessionPullRequest as ReadPullRequest,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import type { SessionGitStatus, SessionPullRequest } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import { useWindowFocus } from './useWindowFocus'

// Git is four local subprocesses, so it can be polled; hive's own TUI refreshes
// on the same cadence. The pull request rides along on each poll and is
// answered from the Go-side cache, so this interval does not set how often
// GitHub is actually asked.
const POLL_INTERVAL_MS = 15_000

/**
 * The attached session's checkout, for the session status bar. Scoped to one
 * session on purpose: a full read costs several git subprocesses, and the bar
 * only ever shows the session in front of you.
 */
export function useSessionStatus(sessionId: Ref<string>): {
  git: Ref<SessionGitStatus | null>
  pullRequest: Ref<SessionPullRequest | null>
  /** Why the pull-request lookup failed, which is never the same as it having none. */
  pullRequestError: Ref<string>
  refresh: (options?: { refreshPullRequest?: boolean }) => Promise<void>
} {
  const git = ref<SessionGitStatus | null>(null)
  const pullRequest = ref<SessionPullRequest | null>(null)
  const pullRequestError = ref('')
  const { focused } = useWindowFocus()

  // Guards a read against the session having changed under it: an answer for
  // the session you just left must not paint over the one you switched to.
  let sequence = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  // The last answer for each session, so switching back to one paints from
  // memory instead of blanking for the ~100ms a git read takes. Without this
  // every tab switch tore the bar down and rebuilt it, which is the flash.
  // Held per composable instance — the Code view mounts once — and bounded by
  // the number of sessions visited in a run, which is small enough not to
  // warrant eviction.
  const lastGit = new Map<string, SessionGitStatus>()
  const lastPullRequest = new Map<string, SessionPullRequest>()

  async function refresh(options: { refreshPullRequest?: boolean } = {}): Promise<void> {
    const id = sessionId.value
    const current = ++sequence
    if (!id) {
      git.value = null
      pullRequest.value = null
      pullRequestError.value = ''
      return
    }

    let status: SessionGitStatus
    try {
      status = await ReadGitStatus(id)
    } catch {
      // A transient failure must not blank a bar that had an answer; the next
      // poll retries.
      return
    }
    if (current !== sequence) return
    git.value = status
    lastGit.set(id, status)

    if (!status.resolved || !status.owner || !status.repo || !status.branch) {
      pullRequest.value = null
      pullRequestError.value = ''
      lastPullRequest.delete(id)
      return
    }
    try {
      const pr = await ReadPullRequest(
        { owner: status.owner, repo: status.repo, branch: status.branch },
        options.refreshPullRequest ?? false,
      )
      if (current !== sequence) return
      pullRequest.value = pr
      pullRequestError.value = ''
      lastPullRequest.set(id, pr)
    } catch (error) {
      if (current !== sequence) return
      // Kept apart from a null pull request: the branch may well have one, and
      // saying it does not would be a claim this failure cannot support.
      pullRequestError.value = error instanceof Error ? error.message : String(error)
    }
  }

  function schedule(): void {
    clearTimeout(timer)
    timer = setTimeout(() => { void poll() }, POLL_INTERVAL_MS)
  }

  async function poll(): Promise<void> {
    if (focused.value) await refresh()
    schedule()
  }

  watch(sessionId, (id) => {
    // Seeded from the last answer for *this* session, never carried over from
    // the one being left: showing another session's branch for a frame would be
    // worse than the blank it replaces. A session not seen yet still starts
    // empty — there is nothing honest to show — so it fills in once.
    const remembered = id ? lastPullRequest.get(id) : undefined
    git.value = (id ? lastGit.get(id) : undefined) ?? null
    // Marked cached so restoring it does not replay the arrival animation: it
    // was known before this paint, which is exactly what `cached` means.
    pullRequest.value = remembered ? { ...remembered, cached: true } : null
    pullRequestError.value = ''
    void refresh()
    schedule()
  }, { immediate: true })

  // A blurred window's checkout keeps changing — an agent is committing in it —
  // so refocusing is the moment the bar is most likely to be stale.
  //
  // It is also the one moment worth spending a request to go behind the pull
  // request cache: you were just somewhere else, and opening a pull request in
  // a browser is the commonest reason to have been. Waiting out the TTL after
  // that reads as the bar not working. Focus is user-driven and infrequent, so
  // this cannot turn into polling GitHub.
  watch(focused, (isFocused) => {
    if (isFocused) void refresh({ refreshPullRequest: true })
  })

  onScopeDispose(() => {
    sequence++
    clearTimeout(timer)
  })

  return { git, pullRequest, pullRequestError, refresh }
}
