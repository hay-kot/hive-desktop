<script setup lang="ts">
import { computed } from 'vue'
import { pullRequestMetadata } from '../lib/itemPresentation'
import IconBadgeCheck from '~icons/lucide/badge-check'
import IconCircleCheck from '~icons/lucide/circle-check'
import IconCircleX from '~icons/lucide/circle-x'
import IconClock3 from '~icons/lucide/clock-3'
import IconGitPullRequestDraft from '~icons/lucide/git-pull-request-draft'
import IconMessageCircleWarning from '~icons/lucide/message-circle-warning'
import type { InboxItem } from '../types/feed'

const props = withDefaults(defineProps<{ item: InboxItem; mode?: 'full' | 'status' | 'changes' }>(), { mode: 'full' })

const metadata = computed(() => pullRequestMetadata(props.item))
const ciLabel = computed(() => {
  switch (metadata.value?.ci) {
    case 'passing': return 'Checks pass'
    case 'pending': return 'Checks pending'
    case 'failing': return 'Checks fail'
    default: return ''
  }
})
const ciIcon = computed(() => {
  switch (metadata.value?.ci) {
    case 'passing': return IconCircleCheck
    case 'failing': return IconCircleX
    default: return IconClock3
  }
})
const ciTone = computed(() => {
  switch (metadata.value?.ci) {
    case 'passing': return 'text-severity-success'
    case 'failing': return 'text-severity-error'
    default: return 'text-severity-warning'
  }
})
const reviewLabel = computed(() => {
  switch (metadata.value?.review) {
    case 'approved': return 'Review approved'
    case 'changes_requested': return 'Changes requested'
    case 'draft': return 'Draft pull request'
    default: return ''
  }
})
const reviewIcon = computed(() => {
  switch (metadata.value?.review) {
    case 'approved': return IconBadgeCheck
    case 'changes_requested': return IconMessageCircleWarning
    default: return IconGitPullRequestDraft
  }
})
const reviewTone = computed(() => {
  switch (metadata.value?.review) {
    case 'approved': return 'text-severity-success'
    case 'changes_requested': return 'text-severity-warning'
    case 'draft': return 'text-text-4'
    default: return 'text-text-3'
  }
})
const hasChanges = computed(() => metadata.value?.additions != null || metadata.value?.deletions != null)
const showStatus = computed(() => props.mode !== 'changes' && ciLabel.value !== '')
const showReview = computed(() => props.mode === 'full' && reviewLabel.value !== '')
const showChanges = computed(() => props.mode !== 'status' && hasChanges.value)
const visible = computed(() => showStatus.value || showReview.value || showChanges.value)
</script>

<template>
  <span v-if="metadata && visible" class="inline-flex shrink-0 items-center gap-2 font-mono text-[10.5px]" data-testid="pr-metadata">
    <span v-if="showStatus" class="inline-flex shrink-0 items-center" :class="ciTone" :aria-label="ciLabel" :title="ciLabel" data-testid="pr-ci">
      <component :is="ciIcon" class="size-3.5" aria-hidden="true" />
    </span>
    <span v-if="showReview" class="inline-flex shrink-0 items-center" :class="reviewTone" :aria-label="reviewLabel" :title="reviewLabel" data-testid="pr-review">
      <component :is="reviewIcon" class="size-3.5" aria-hidden="true" />
    </span>
    <span v-if="showChanges" class="inline-flex shrink-0 items-center gap-1" aria-label="Pull request line changes" data-testid="pr-lines">
      <span class="text-severity-success">+{{ metadata.additions ?? 0 }}</span>
      <span class="text-severity-error">−{{ metadata.deletions ?? 0 }}</span>
    </span>
  </span>
</template>
