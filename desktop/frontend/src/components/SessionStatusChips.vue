<script setup lang="ts">
// The session half of the status bar: what the checkout looks like, and what
// its branch's pull request is doing. Session-only — a chat has no branch —
// which is why it sits in PaneStatusBar's slot rather than in the bar itself.
import { computed } from 'vue'
import { Browser } from '@wailsio/runtime'
// The two git-state glyphs are chosen as a pair rather than each on its own
// merits. Both are a container with a mark on it — a file carrying its changes,
// a tray with something leaving it — so they share a silhouette and a density
// and read as two of the same kind of thing. Mixing a solid glyph with a bare
// stroke does not, whichever two you pick.
import IconCheck from '~icons/lucide/check'
import IconCopy from '~icons/lucide/copy'
import IconFileDiff from '~icons/lucide/file-diff'
import IconGitBranch from '~icons/lucide/git-branch'
import IconGitPullRequest from '~icons/lucide/git-pull-request'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconUpload from '~icons/lucide/upload'
import AppTooltip from './AppTooltip.vue'
import { useClipboard } from '../composables/useClipboard'
import { markdownPullRequestLink } from '../lib/prLink'
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

// A cached pull request was known before the bar painted, so there is nothing
// to announce; a failed lookup did just arrive, so it animates like a fresh one.
const animateArrival = computed(() => !props.pullRequest?.cached)

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

function openPullRequest(): void {
  const url = pr.value?.url
  if (url) void Browser.OpenURL(url)
}

const { copy, copied } = useClipboard()

async function copyLink(): Promise<void> {
  const found = pr.value
  // The repository name alone, as the shell script used: the owner is already
  // implied by wherever this is being pasted.
  const repo = props.git?.repo
  if (!found || !repo) return
  await copy(markdownPullRequestLink(found, repo))
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
       pull request and its copy buttons — stays whole. It carries no tooltip,
       so a name cut this way cannot be read back. -->
  <div v-if="git" class="flex min-w-0 items-center text-[11px]" data-testid="session-status-chips">
    <div v-if="showGitGroup" class="flex min-w-0 items-center gap-1">
      <span
        v-if="showBranch"
        class="flex h-6 min-w-0 items-center gap-1 px-1.5 text-text-3"
        data-testid="session-status-branch"
      >
        <IconGitBranch class="size-3 shrink-0 text-text-4" aria-hidden="true" />
        <span class="truncate font-mono">{{ git.branch }}</span>
      </span>

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

    <!-- Animated only when the answer actually came off the network. A cached
         one is already known by the time the bar paints, so fading it in would
         animate nothing arriving — the reason the backend reports `cached` at
         all. Enter only: a leave transition would make switching sessions
         flicker. -->
    <Transition :css="animateArrival" name="pr-arrive">
      <div v-if="showPRGroup" class="flex shrink-0 items-center">
        <!-- A rule, not a wider gap. Everything left of here describes the
             checkout and everything right of it the remote, and a gap cannot
             say that — it only reads as uneven spacing, which is how this row
             looked before.
             It sits outside the gap-1 group below on purpose: as a child of it
             the group's gap would land on the rule's right only, and mx-2 would
             read as 8px left and 12px right. -->
        <span v-if="showGitGroup" class="mx-2 h-3.5 w-px shrink-0 bg-border" aria-hidden="true" />

        <div class="flex shrink-0 items-center gap-1">
        <!-- h-6/rounded-[7px] is PaneStatusBar's button metric: these sit in the
             same row as the editor and Finder buttons, so a hover rect of a
             different height or corner reads as a mistake. -->
        <button
          v-if="pr"
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

      <!-- One button, one format. Both were offered when the shape was still
           in question; the Markdown link is the one that gets used, so the
           second was width spent on a choice nobody was making. It is labelled
           plainly as copy — its tooltip says what lands on the clipboard. -->
      <AppTooltip v-if="pr" :text="copied ? 'Copied' : 'Copy link to this pull request'">
        <button
          type="button"
          class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-4 hover:bg-chip hover:text-text"
          :class="{ 'text-severity-success': copied }"
          aria-label="Copy link to this pull request"
          data-testid="session-status-copy"
          @click="copyLink"
        >
          <IconCheck v-if="copied" class="size-3.5" />
          <IconCopy v-else class="size-3.5" />
        </button>
      </AppTooltip>

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
    </Transition>
  </div>
</template>

<style scoped>
/* Short and small: the row is chrome, and anything longer or further than this
   pulls the eye off whatever the terminal below is doing. Enter only — nothing
   animates on the way out, so switching sessions swaps cleanly. */
.pr-arrive-enter-active {
  transition: opacity 180ms ease-out, transform 180ms ease-out;
}

.pr-arrive-enter-from {
  opacity: 0;
  transform: translateX(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .pr-arrive-enter-active {
    transition: none;
  }
}
</style>
