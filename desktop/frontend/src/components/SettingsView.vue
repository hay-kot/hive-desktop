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
import grafanaIcon from '../assets/integrations/grafana.svg'
import posthogIcon from '../assets/integrations/posthog.svg'
import slackIcon from '../assets/integrations/slack.svg'
import GithubIntegrationDrawer from './settings/GithubIntegrationDrawer.vue'
import WebhookIntegrationDrawer from './settings/WebhookIntegrationDrawer.vue'
import SettingsLayout from './settings/SettingsLayout.vue'
import SettingsNavItem from './settings/SettingsNavItem.vue'
import SettingsSection from './settings/SettingsSection.vue'
import SettingsSegmented from './settings/SettingsSegmented.vue'
import IconWebhook from '~icons/lucide/webhook'
import { setTheme, themeLabels, themes, useTheme, type Theme } from '../composables/useTheme'
import { useWebhookSettings } from '../composables/useWebhookSettings'
import { applicationSettingsSections, type ApplicationSettingsSection } from '../router'

const props = withDefaults(defineProps<{
  githubConnected: boolean
  githubLogin?: string
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
const futureIntegrations = [
  { id: 'grafana', name: 'Grafana', description: 'Metrics, dashboards, and alerts', icon: grafanaIcon },
  { id: 'posthog', name: 'PostHog', description: 'Product analytics and events', icon: posthogIcon },
  { id: 'slack', name: 'Slack', description: 'Messages and notifications', icon: slackIcon },
]

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
          description="Connections bring external events into Hive. More providers will support guided setup here as they become available."
          class="mb-5"
        />

        <div class="flex flex-col gap-3">
          <BaseCard class="rounded-lg border border-border bg-raised" data-testid="integration-github">
            <template #icon>
              <BaseIconBadge :size="40" rounded="rounded-lg" class="bg-white p-2">
                <img :src="githubIcon" alt="GitHub" class="size-full" />
              </BaseIconBadge>
            </template>
            <div class="min-w-0 flex-1">
              <div class="text-[13.5px] font-semibold text-text">GitHub</div>
              <div class="mt-0.5 truncate text-xs text-text-3">{{ props.githubLogin ? `Connected as ${props.githubLogin}` : 'Issues, pull requests, and notifications' }}</div>
            </div>
            <template #actions>
              <div class="flex shrink-0 items-center gap-2">
                <BaseBadge
                  :tone="props.githubConnected ? 'success' : 'neutral'"
                  variant="pill"
                  class="px-2.5 py-1 text-[11px] font-semibold"
                  data-testid="integration-github-status"
                >{{ props.githubConnected ? 'Connected' : 'Not connected' }}</BaseBadge>
                <button
                  type="button"
                  class="flex size-7 cursor-pointer items-center justify-center rounded-md text-text-3 hover:bg-chip hover:text-text"
                  aria-label="Configure GitHub integration"
                  data-testid="integration-github-configure"
                  @click="githubSettingsOpen = true"
                ><IconSettings class="size-3.5" /></button>
              </div>
            </template>
          </BaseCard>

          <BaseCard class="rounded-lg border border-border bg-raised" data-testid="integration-webhook">
            <template #icon>
              <BaseIconBadge :size="40" rounded="rounded-lg" class="bg-chip p-2 text-text-2">
                <IconWebhook class="size-full" />
              </BaseIconBadge>
            </template>
            <div class="min-w-0 flex-1">
              <div class="text-[13.5px] font-semibold text-text">Webhooks</div>
              <div class="mt-0.5 truncate text-xs text-text-3">{{ webhookDescription }}</div>
            </div>
            <template #actions>
              <div class="flex shrink-0 items-center gap-2">
                <BaseBadge
                  :tone="webhookStatus.tone"
                  variant="pill"
                  class="px-2.5 py-1 text-[11px] font-semibold"
                  data-testid="integration-webhook-status"
                >{{ webhookStatus.label }}</BaseBadge>
                <button
                  type="button"
                  class="flex size-7 cursor-pointer items-center justify-center rounded-md text-text-3 hover:bg-chip hover:text-text"
                  aria-label="Configure webhook listener"
                  data-testid="integration-webhook-configure"
                  @click="webhookSettingsOpen = true"
                ><IconSettings class="size-3.5" /></button>
              </div>
            </template>
          </BaseCard>

          <BaseCard
            v-for="integration in futureIntegrations"
            :key="integration.id"
            class="rounded-lg border border-border bg-raised"
            :data-testid="`integration-${integration.id}`"
          >
            <template #icon>
              <BaseIconBadge :size="40" rounded="rounded-lg" class="bg-white p-2">
                <img :src="integration.icon" :alt="integration.name" class="size-full object-contain" />
              </BaseIconBadge>
            </template>
            <div class="min-w-0 flex-1">
              <div class="text-[13.5px] font-semibold text-text">{{ integration.name }}</div>
              <div class="mt-0.5 truncate text-xs text-text-3">{{ integration.description }}</div>
            </div>
            <template #actions>
              <div class="flex shrink-0 items-center gap-2.5">
                <span class="font-mono text-[10.5px] text-text-4">Coming soon</span>
                <button
                  type="button"
                  disabled
                  class="cursor-not-allowed rounded-md border border-border px-2.5 py-1.5 text-[11.5px] font-medium text-text-4 opacity-60"
                  :data-testid="`integration-${integration.id}-add`"
                >Add connection</button>
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
