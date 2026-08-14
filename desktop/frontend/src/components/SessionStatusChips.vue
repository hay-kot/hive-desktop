<script setup lang="ts">
// The session half of the status bar: what the checkout looks like, and what
// its branch's pull request is doing. Session-only — a chat has no branch —
// which is why it sits in PaneStatusBar's slot rather than in the bar itself.
import { computed } from 'vue'
import { Browser } from '@wailsio/runtime'
import IconCheck from '~icons/lucide/check'
import IconCircleDot from '~icons/lucide/circle-dot'
import IconGitBranch from '~icons/lucide/git-branch'
import IconGitPullRequest from '~icons/lucide/git-pull-request'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconUpload from '~icons/lucide/upload'
import IconX from '~icons/lucide/x'
import type { SessionGitStatus, SessionPullRequest } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const props = defineProps<{
  git: SessionGitStatus | null
  pullRequest: SessionPullRequest | null
  /** Why the pull-request lookup failed; shown instead of a PR chip. */
  pullRequestError: string
}>()

const emit = defineEmits<{ 'refresh-pull-request': [] }>()

const showBranch = computed(() => !!props.git?.resolved && !!props.git.branch)
const diff = computed(() => {
  const git = props.git
  if (!git?.resolved) return ''
  if (!git.additions && !git.deletions) return ''
  return `+${git.additions} −${git.deletions}`
})

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
</script>

<template>
  <div v-if="git" class="flex min-w-0 items-center gap-2 text-[11px]" data-testid="session-status-chips">
    <span
      v-if="showBranch"
      class="flex min-w-0 items-center gap-1 text-text-3"
      :title="git.path"
      data-testid="session-status-branch"
    >
      <IconGitBranch class="size-3 shrink-0 text-text-4" aria-hidden="true" />
      <span class="truncate font-mono">{{ git.branch }}</span>
    </span>

    <span
      v-if="git.resolved && git.dirty"
      class="shrink-0 text-severity-warning"
      title="Uncommitted changes"
      data-testid="session-status-dirty"
    >●</span>

    <span
      v-if="diff"
      class="shrink-0 font-mono text-text-4"
      title="Lines changed against the default branch"
      data-testid="session-status-diff"
    >{{ diff }}</span>

    <IconUpload
      v-if="git.resolved && git.unpushed"
      class="size-3 shrink-0 text-text-4"
      aria-label="Unpushed commits"
      data-testid="session-status-unpushed"
    />

    <!-- The git read failed. Saying so beats a bar that silently reports a
         clean branch it never managed to look at. -->
    <span
      v-if="git.error"
      class="flex shrink-0 items-center gap-1 text-severity-error"
      :title="git.error"
      data-testid="session-status-git-error"
    ><IconTriangleAlert class="size-3" aria-hidden="true" />git</span>

    <button
      v-if="pr"
      type="button"
      class="flex shrink-0 cursor-pointer items-center gap-1 rounded-[6px] px-1 hover:bg-chip"
      :class="prTone"
      :title="prTitle"
      data-testid="session-status-pr"
      @click="openPullRequest"
    >
      <IconGitPullRequest class="size-3" aria-hidden="true" />
      <span class="font-mono">{{ prLabel }}</span>
      <IconCheck v-if="pr.checks === 'passing'" class="size-3 text-severity-success" aria-label="Checks passing" />
      <IconX v-else-if="pr.checks === 'failing'" class="size-3 text-severity-error" aria-label="Checks failing" />
      <IconCircleDot v-else-if="pr.checks === 'pending'" class="size-3 text-severity-warning" aria-label="Checks pending" />
    </button>

    <!-- A failed lookup, never rendered as "no pull request": the branch may
         well have one, and claiming otherwise is a fact this cannot support. -->
    <button
      v-else-if="pullRequestError"
      type="button"
      class="flex shrink-0 cursor-pointer items-center gap-1 rounded-[6px] px-1 text-severity-error hover:bg-chip"
      :title="`${pullRequestError} — click to retry`"
      data-testid="session-status-pr-error"
      @click="emit('refresh-pull-request')"
    ><IconTriangleAlert class="size-3" aria-hidden="true" />PR</button>
  </div>
</template>
