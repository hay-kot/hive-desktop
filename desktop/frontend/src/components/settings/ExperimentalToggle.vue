<script setup lang="ts">
// The control cluster for a ships-dark opt-in: what it costs to turn on stated
// once, in the same shape, wherever one appears. Experimental is a posture a
// feature holds, not a category of settings, so each opt-in sits on the pane
// for the feature it gates rather than in a shared Experimental list.
//
// The flags are read once at startup (ADRs terminal-experimental-gate, a-workspace-declares-its-own-authority), so a change is pending
// until the app is relaunched — `restartPending` is what says so.
import AppSwitch from '../AppSwitch.vue'
import BaseBadge from '../BaseBadge.vue'

defineProps<{
  modelValue: boolean
  restartPending: boolean
  ariaLabel: string
  testid: string
}>()
defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<template>
  <div class="flex items-center gap-2.5">
    <BaseBadge tone="neutral" variant="pill" class="shrink-0 px-2 py-0.5 text-[10.5px] font-semibold uppercase">Experimental</BaseBadge>
    <span
      v-if="restartPending"
      class="shrink-0 rounded-full border border-severity-info-border bg-severity-info-tint px-2 py-0.5 text-[11px] font-medium text-severity-info"
      :data-testid="`${testid}-restart`"
    >Restart to apply</span>
    <AppSwitch
      :model-value="modelValue"
      :aria-label="ariaLabel"
      :testid="testid"
      @update:model-value="$emit('update:modelValue', $event)"
    />
  </div>
</template>
