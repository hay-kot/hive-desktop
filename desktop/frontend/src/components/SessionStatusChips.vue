<script setup lang="ts">
// The session half of the status bar: what the checkout looks like, and what
// its branch's pull request is doing. Session-only — a chat has no branch —
// which is why it sits in PaneStatusBar's slot rather than in the bar itself.
import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
// The two git-state glyphs are chosen as a pair rather than each on its own
// merits. Both are a container with a mark on it — a file carrying its changes,
// a tray with something leaving it — so they share a silhouette and a density
// and read as two of the same kind of thing. Mixing a solid glyph with a bare
// stroke does not, whichever two you pick.
import IconCheck from '~icons/lucide/check'
import IconFileDiff from '~icons/lucide/file-diff'
import IconGitBranch from '~icons/lucide/git-branch'
import IconGitPullRequest from '~icons/lucide/git-pull-request'
import IconLink from '~icons/lucide/link'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconType from '~icons/lucide/type'
import IconUpload from '~icons/lucide/upload'
import AppTooltip from './AppTooltip.vue'
import { useClipboard } from '../composables/useClipboard'
import { markdownPullRequestLink, plainPullRequestLink } from '../lib/prLink'
import type { SessionGitStatus, SessionPullRequest } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const props = defineProps<{
  git: SessionGitStatus | null
  pullRequest: SessionPullRequest | null
  /** Why the pull-request lookup failed; shown instead of a PR chip. */
  pullRequestError: string
}>()

const emit = defineEmits<{ 'refresh-pull-request': [] }>()

const showBranch = computed(() => !!props.git?.resolved && !!props.git.branch)
// Split rather than one string: additions and deletions are coloured
// separately, the way every diff the user reads elsewhere colours them.
const showDiff = computed(() => !!props.git?.resolved && !!(props.git.additions || props.git.deletions))

// The row's two halves: what the checkout looks like, and what the remote has
// to say about it. Tracked so the rule between them appears only when it has
// something on both sides to separate.
const showGitGroup = computed(() => {
  const git = props.git
  if (!git) return false
  return showBranch.value || showDiff.value || !!git.error || (git.resolved && (git.dirty || git.unpushed))
})
const showPRGroup = computed(() => props.pullRequest?.status === 'found' || !!props.pullRequestError)

const branchTooltip = computed(() => [props.git?.branch, props.git?.path].filter(Boolean).join('\n'))

// Only a found pull request has anything to render; the other statuses say why
// there is none, and none of them is worth a chip of its own — a branch with no
// PR is the normal state of a session that has not pushed yet.
const pr = computed(() => (props.pullRequest?.status === 'found' ? props.pullRequest : null))

const prLabel = computed(() => {
  const found = pr.value
  if (!found) return ''
  if (found.state === 'MERGED') return `#${found.number} merged`
  if (found.state === 'CLOSED') return `#${found.number} closed`
  return found.isDraft ? `#${found.number} draft` : `#${found.number}`
})

const prTone = computed(() => {
  const found = pr.value
  if (!found) return 'text-text-3'
  if (found.state === 'MERGED') return 'text-accent'
  if (found.state === 'CLOSED') return 'text-text-4'
  if (found.reviewDecision === 'CHANGES_REQUESTED') return 'text-severity-warning'
  if (found.reviewDecision === 'APPROVED') return 'text-severity-success'
  return 'text-text-3'
})

const checksTone = computed(() => {
  switch (pr.value?.checks) {
    case 'passing': return 'text-severity-success'
    case 'failing': return 'text-severity-error'
    default: return 'text-severity-warning'
  }
})

const prTitle = computed(() => {
  const found = pr.value
  if (!found) return ''
  const parts = [found.title || `Pull request #${found.number}`]
  if (found.reviewDecision) parts.push(found.reviewDecision.toLowerCase().replace(/_/g, ' '))
  if (found.checks) parts.push(`checks ${found.checks}`)
  return parts.join(' · ')
})

function openPullRequest(): void {
  const url = pr.value?.url
  if (url) void Browser.OpenURL(url)
}

// Which button last copied, so only that one shows the tick. A shared boolean
// would flash both, which reads as having copied something twice.
const { copy, copied } = useClipboard()
const copiedFrom = ref<'markdown' | 'plain' | ''>('')
const copiedFormat = computed(() => (copied.value ? copiedFrom.value : ''))

async function copyLink(format: 'markdown' | 'plain'): Promise<void> {
  const found = pr.value
  // The repository name alone, as the shell script used: the owner is already
  // implied by wherever this is being pasted.
  const repo = props.git?.repo
  if (!found || !repo) return
  copiedFrom.value = format
  await copy(format === 'markdown' ? markdownPullRequestLink(found, repo) : plainPullRequestLink(found, repo))
}
</script>

<template>
  <!-- One metric for every item in this row: a 24px-tall box, px-1.5 when it
       carries text and a 24px square when it is icon-only, plus a gap-1 between
       them. Before this the text chips were bare and only the clickable ones
       had a box, so nothing shared a baseline or an edge and the row read as
       loose parts.
       Overflow: the branch is the only item allowed to shrink, so a narrow pane
       truncates the branch name and everything else — diff, state icons, the
       pull request and its copy buttons — stays whole. Its tooltip carries the
       full name for exactly that reason. -->
  <div v-if="git" class="flex min-w-0 items-center text-[11px]" data-testid="session-status-chips">
    <div v-if="showGitGroup" class="flex min-w-0 items-center gap-1">
      <!-- The branch name leads the tooltip because this is the item that
           truncates: once the pane is narrow enough to cut it, hovering is the
           only way left to read it. -->
      <AppTooltip v-if="showBranch" :text="branchTooltip" class="min-w-0">
        <span class="flex h-6 min-w-0 items-center gap-1 px-1.5 text-text-3" data-testid="session-status-branch">
          <IconGitBranch class="size-3 shrink-0 text-text-4" aria-hidden="true" />
          <span class="truncate font-mono">{{ git.branch }}</span>
        </span>
      </AppTooltip>

      <AppTooltip v-if="showDiff" text="Lines changed against the default branch">
        <span class="flex h-6 shrink-0 items-center gap-1 px-1.5 font-mono" data-testid="session-status-diff">
          <span class="text-severity-success">+{{ git.additions }}</span>
          <span class="text-severity-error">−{{ git.deletions }}</span>
        </span>
      </AppTooltip>

      <!-- Icon-only, each explained by its tooltip alone — which is why these
           use AppTooltip rather than `title`. An icon nothing explains is what
           made the first version of this row read as a set of buttons, and a
           `title` that takes a second and a half to appear is barely better
           than none. -->
      <AppTooltip v-if="git.resolved && git.dirty" text="Uncommitted changes in this checkout">
        <span
          class="flex size-6 shrink-0 items-center justify-center text-severity-warning"
          role="img"
          aria-label="Uncommitted changes in this checkout"
          data-testid="session-status-dirty"
        ><IconFileDiff class="size-3.5" /></span>
      </AppTooltip>

      <AppTooltip v-if="git.resolved && git.unpushed" text="Commits on this branch that the remote does not have">
        <span
          class="flex size-6 shrink-0 items-center justify-center text-text-3"
          role="img"
          aria-label="Commits on this branch that the remote does not have"
          data-testid="session-status-unpushed"
        ><IconUpload class="size-3.5" /></span>
      </AppTooltip>

      <!-- The git read failed. Saying so beats a bar that silently reports a
           clean branch it never managed to look at. -->
      <AppTooltip v-if="git.error" :text="git.error">
        <span
          class="flex h-6 shrink-0 items-center gap-1 px-1.5 text-severity-error"
          data-testid="session-status-git-error"
        ><IconTriangleAlert class="size-3" aria-hidden="true" />git failed</span>
      </AppTooltip>
    </div>

    <!-- A rule, not a wider gap. Everything left of here describes the
         checkout and everything right of it the remote, and a gap cannot say
         that — it only reads as uneven spacing, which is how this row looked
         before. -->
    <span v-if="showGitGroup && showPRGroup" class="mx-2 h-3.5 w-px shrink-0 bg-border" aria-hidden="true" />

    <div v-if="showPRGroup" class="flex shrink-0 items-center gap-1">
      <!-- h-6/rounded-[7px] is PaneStatusBar's button metric: these sit in the
           same row as the editor and Finder buttons, so a hover rect of a
           different height or corner reads as a mistake. -->
      <AppTooltip v-if="pr" :text="prTitle">
        <button
          type="button"
          class="flex h-6 shrink-0 cursor-pointer items-center gap-1 rounded-[7px] px-1.5 hover:bg-chip"
          :class="prTone"
          data-testid="session-status-pr"
          @click="openPullRequest"
        >
          <IconGitPullRequest class="size-3" aria-hidden="true" />
          <span class="font-mono">{{ prLabel }}</span>
          <!-- The check state is a word for the same reason the git states
               above are: a tick, a cross and a dot are three glyphs the reader
               has to learn, and "passing" is none. -->
          <span v-if="pr.checks" :class="checksTone" data-testid="session-status-checks">{{ pr.checks }}</span>
        </button>
      </AppTooltip>

      <!-- Both formats side by side rather than one button and a setting
           deciding what it produces: which one you want depends on where you
           are pasting, so a stored preference is a trip to Settings before
           every other paste. Flush against the chip because they act on it — a
           gap here would read as three unrelated controls. -->
      <template v-if="pr">
        <AppTooltip :text="copiedFormat === 'markdown' ? 'Copied' : 'Copy Markdown link'">
          <button
            type="button"
            class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-4 hover:bg-chip hover:text-text"
            :class="{ 'text-severity-success': copiedFormat === 'markdown' }"
            aria-label="Copy Markdown link"
            data-testid="session-status-copy-markdown"
            @click="copyLink('markdown')"
          >
            <IconCheck v-if="copiedFormat === 'markdown'" class="size-3.5" />
            <IconLink v-else class="size-3.5" />
          </button>
        </AppTooltip>
        <AppTooltip :text="copiedFormat === 'plain' ? 'Copied' : 'Copy plain text link'">
          <button
            type="button"
            class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-4 hover:bg-chip hover:text-text"
            :class="{ 'text-severity-success': copiedFormat === 'plain' }"
            aria-label="Copy plain text link"
            data-testid="session-status-copy-plain"
            @click="copyLink('plain')"
          >
            <IconCheck v-if="copiedFormat === 'plain'" class="size-3.5" />
            <IconType v-else class="size-3.5" />
          </button>
        </AppTooltip>
      </template>

      <!-- A failed lookup, never rendered as "no pull request": the branch may
           well have one, and claiming otherwise is a fact this cannot support. -->
      <AppTooltip v-else-if="pullRequestError" :text="`${pullRequestError} — click to retry`">
        <button
          type="button"
          class="flex h-6 shrink-0 cursor-pointer items-center gap-1 rounded-[7px] px-1.5 text-severity-error hover:bg-chip"
          data-testid="session-status-pr-error"
          @click="emit('refresh-pull-request')"
        ><IconTriangleAlert class="size-3" aria-hidden="true" />PR failed</button>
      </AppTooltip>
    </div>
  </div>
</template>
