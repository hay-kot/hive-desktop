<script setup lang="ts">
// Application-wide settings, opened from the persistent profile rail.
// Only settings backed by real behavior or explicitly marked future
// integrations belong here.
import { computed, ref, watch, type Component } from 'vue'
import IconKeyboard from '~icons/lucide/keyboard'
import IconPalette from '~icons/lucide/palette'
import IconPlug from '~icons/lucide/plug'
import IconPlay from '~icons/lucide/play'
import IconHardDrive from '~icons/lucide/hard-drive'
import IconBell from '~icons/lucide/bell'
import IconSettings from '~icons/lucide/settings'
import IconSparkles from '~icons/lucide/sparkles'
import BaseBadge from './BaseBadge.vue'
import BaseCard from './BaseCard.vue'
import BaseIconBadge from './BaseIconBadge.vue'
import ActionSettingsView from './ActionSettingsView.vue'
import KeybindingSettingsView from './KeybindingSettingsView.vue'
import PromptSettingsView from './PromptSettingsView.vue'
import SystemSettingsView from './SystemSettingsView.vue'
import NotificationSettingsView from './NotificationSettingsView.vue'
import githubIcon from '../assets/integrations/github.svg'
import GithubIntegrationDrawer from './settings/GithubIntegrationDrawer.vue'
import WebhookIntegrationDrawer from './settings/WebhookIntegrationDrawer.vue'
import SettingsLayout from './settings/SettingsLayout.vue'
import SettingsNavItem from './settings/SettingsNavItem.vue'
import SettingsSection from './settings/SettingsSection.vue'
import SettingsSegmented from './settings/SettingsSegmented.vue'
import IconWebhook from '~icons/lucide/webhook'
import { setTheme, themeLabels, themes, useTheme, type Theme } from '../composables/useTheme'
import { useWebhookSettings } from '../composables/useWebhookSettings'
import { isConnected, takesCredential, useIntegrations } from '../composables/useIntegrations'
import type { Integration } from '../types/integrations'
import { applicationSettingsSections, type ApplicationSettingsSection } from '../router'

const props = withDefaults(defineProps<{
  activeCategory: ApplicationSettingsSection
  knownFeedTypes?: string[]
}>(), { knownFeedTypes: () => [] })
const emit = defineEmits<{ close: []; 'select-category': [category: ApplicationSettingsSection] }>()
// Keyed by section id and ordered by router.ts's applicationSettingsSections,
// so a section added there shows up here (and TypeScript flags the missing
// entry) instead of being routable but absent from the nav.
const categoryMeta: Record<ApplicationSettingsSection, { label: string; title: string; icon: Component }> = {
  appearance: { label: 'Appearance', title: 'Appearance', icon: IconPalette },
  keybindings: { label: 'Keyboard', title: 'Keyboard shortcuts', icon: IconKeyboard },
  integrations: { label: 'Integrations', title: 'Integrations', icon: IconPlug },
  actions: { label: 'Actions', title: 'Actions', icon: IconPlay },
  prompts: { label: 'LLM prompts', title: 'LLM prompts', icon: IconSparkles },
  system: { label: 'System', title: 'System', icon: IconHardDrive },
  notifications: { label: 'Notifications', title: 'Notifications', icon: IconBell },
}
const categories = applicationSettingsSections.map((id) => ({ id, ...categoryMeta[id] }))
const sectionTitle = computed(() => categoryMeta[props.activeCategory].title)

const { theme } = useTheme()
const themeOptions = themes.map((value) => ({ value, label: themeLabels[value] }))
const githubSettingsOpen = ref(false)
const webhookSettingsOpen = ref(false)

// The webhook card's badge reflects the same state the drawer edits, so a save
// there is reflected here without a second fetch.
const { settings: webhook, refresh: refreshWebhook } = useWebhookSettings()
const webhookStatus = computed(() => {
  if (!webhook.value) return { label: 'Local', tone: 'neutral' as const }
  // A saved change the listener has not picked up yet outranks what it is
  // currently doing — otherwise disabling it would still read "Running".
  if (webhook.value.startError) return { label: 'Port in use', tone: 'danger' as const }
  if (webhook.value.restartRequired) return { label: 'Restart needed', tone: 'neutral' as const }
  if (webhook.value.running) return { label: 'Running', tone: 'success' as const }
  return { label: 'Disabled', tone: 'neutral' as const }
})
const webhookDescription = computed(() => webhook.value
  ? `Receive JSON from anything that can POST — ${webhook.value.baseUrl}`
  : 'Receive JSON from anything that can POST to a local endpoint')

watch(() => props.activeCategory, (category) => {
  if (category === 'integrations') void refreshWebhook()
}, { immediate: true })
// Cards come from the Go connector registry, so adding a connector adds a
// card. Only its presentation is here — a type the registry reports but this
// map has not met still renders, with a generic icon and no blurb, rather
// than being silently dropped.
const { integrations, loaded: integrationsLoaded } = useIntegrations()

const presentation: Record<string, { description: string }> = {
  'sources.github': { description: 'Issues, pull requests, and notifications' },
  'sources.webhook': { description: 'Receive JSON from anything that can POST' },
}

// The drawer each card's gear opens. A connector with no drawer yet gets no
// gear rather than a button that does nothing.
const drawers: Record<string, () => void> = {
  'sources.github': () => { githubSettingsOpen.value = true },
  'sources.webhook': () => { webhookSettingsOpen.value = true },
}

function subtitleFor(integration: Integration): string {
  // The webhook listener's own state is richer than "connected" and is what
  // its card has always shown; it has no credential to describe.
  if (integration.type === 'sources.webhook') return webhookDescription.value
  if (integration.envOverride && integration.accounts.length === 0) {
    return `Connected via ${envOverrideName(integration.provider)}`
  }
  if (integration.accounts.length > 0) return `Connected as ${integration.accounts.join(', ')}`
  return presentation[integration.type]?.description ?? ''
}

// Mirrors credentials.EnvOverrideName in Go. Shown so a headless or CI run
// explains why it is connected with no account listed.
function envOverrideName(provider: string): string {
  return `HIVE_${provider.toUpperCase().replace(/[^A-Z0-9]/g, '_')}_TOKEN`
}

// Cards are keyed by the connector's bare name, not its namespaced type:
// "integration-github" reads better in a selector than
// "integration-sources.github", and the namespace is constant across every
// entry here so it carries no information.
function cardId(type: string): string {
  return type.replace(/^sources\./, '')
}

function statusFor(integration: Integration): { label: string; tone: 'success' | 'neutral' | 'danger' } {
  if (integration.type === 'sources.webhook') return webhookStatus.value
  if (!takesCredential(integration)) return { label: 'Local', tone: 'neutral' }
  return isConnected(integration)
    ? { label: 'Connected', tone: 'success' }
    : { label: 'Not connected', tone: 'neutral' }
}

function onThemeChange(value: string): void {
  setTheme(value as Theme)
}
</script>

<template>
  <SettingsLayout close-testid="settings-close" data-testid="settings-view" @close="emit('close')">
    <template #sidebar-title>
      <div class="text-[15px] font-semibold tracking-[-.01em] text-text">Application settings</div>
    </template>
    <template #nav>
      <SettingsNavItem
        v-for="category in categories"
        :key="category.id"
        :active="props.activeCategory === category.id"
        :icon="category.icon"
        :label="category.label"
        :testid="`settings-category-${category.id}`"
        @select="emit('select-category', category.id)"
      />
    </template>
    <template #header-title>
      <span class="text-[13px] font-semibold text-text">{{ sectionTitle }}</span>
    </template>

    <div class="hive-scroll min-h-0 flex-1 overflow-y-auto px-6 py-6">
      <div v-if="props.activeCategory === 'appearance'" class="mx-auto max-w-[560px]">
        <SettingsSegmented
          :model-value="theme"
          label="Theme"
          :options="themeOptions"
          hint="Applies immediately across the whole app."
          testid="settings-theme-toggle"
          @update:model-value="onThemeChange"
        />
      </div>

      <KeybindingSettingsView v-else-if="props.activeCategory === 'keybindings'" />

      <ActionSettingsView v-else-if="props.activeCategory === 'actions'" :known-types="props.knownFeedTypes" />

      <PromptSettingsView v-else-if="props.activeCategory === 'prompts'" />

      <SystemSettingsView v-else-if="props.activeCategory === 'system'" />

      <NotificationSettingsView v-else-if="props.activeCategory === 'notifications'" />

      <div v-else class="mx-auto max-w-[640px]" data-testid="settings-integrations">
        <SettingsSection
          title="Data sources"
          description="Connections bring external events into Hive. Every connector the app knows about is listed here."
          class="mb-5"
        />

        <div v-if="!integrationsLoaded" class="font-mono text-xs text-text-4" data-testid="integrations-loading">Loading…</div>
        <div v-else class="flex flex-col gap-3">
          <BaseCard
            v-for="integration in integrations"
            :key="integration.type"
            class="flex-wrap items-start rounded-lg border border-border bg-raised @[600px]/pane:flex-nowrap @[600px]/pane:items-center"
            :data-testid="`integration-${cardId(integration.type)}`"
          >
            <template #icon>
              <BaseIconBadge :size="40" rounded="rounded-lg" :class="integration.type === 'sources.github' ? 'bg-white p-2' : 'bg-chip p-2 text-text-2'">
                <img v-if="integration.type === 'sources.github'" :src="githubIcon" alt="" class="size-full" />
                <IconWebhook v-else-if="integration.type === 'sources.webhook'" class="size-full" />
                <IconPlug v-else class="size-full" />
              </BaseIconBadge>
            </template>
            <div class="min-w-0 flex-1">
              <div class="text-[13.5px] font-semibold text-text">{{ integration.title }}</div>
              <div class="mt-0.5 truncate text-xs text-text-3">{{ subtitleFor(integration) }}</div>
            </div>
            <template #actions>
              <div class="flex w-full items-center justify-end gap-2 @[600px]/pane:w-auto @[600px]/pane:shrink-0">
                <BaseBadge
                  v-if="integration.stability !== 'stable'"
                  tone="neutral"
                  variant="pill"
                  class="px-2 py-1 text-[10.5px] font-semibold uppercase"
                  :data-testid="`integration-${cardId(integration.type)}-stability`"
                >{{ integration.stability }}</BaseBadge>
                <BaseBadge
                  :tone="statusFor(integration).tone"
                  variant="pill"
                  class="px-2.5 py-1 text-[11px] font-semibold"
                  :data-testid="`integration-${cardId(integration.type)}-status`"
                >{{ statusFor(integration).label }}</BaseBadge>
                <button
                  v-if="drawers[integration.type]"
                  type="button"
                  class="flex size-7 cursor-pointer items-center justify-center rounded-md text-text-3 hover:bg-chip hover:text-text"
                  :aria-label="`Configure ${integration.title}`"
                  :data-testid="`integration-${cardId(integration.type)}-configure`"
                  @click="drawers[integration.type]()"
                ><IconSettings class="size-3.5" /></button>
              </div>
            </template>
          </BaseCard>
        </div>
        <GithubIntegrationDrawer v-if="githubSettingsOpen" @close="githubSettingsOpen = false" />
        <WebhookIntegrationDrawer v-if="webhookSettingsOpen" @close="webhookSettingsOpen = false" />
      </div>
    </div>
  </SettingsLayout>
</template>
