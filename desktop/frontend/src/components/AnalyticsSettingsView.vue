<script setup lang="ts">
import { Browser } from '@wailsio/runtime'
import { onMounted, ref } from 'vue'
import IconBookOpen from '~icons/lucide/book-open'
import AppSwitch from './AppSwitch.vue'
import BaseBadge from './BaseBadge.vue'
import SettingsError from './settings/SettingsError.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsRow from './settings/SettingsRow.vue'
import SettingsSection from './settings/SettingsSection.vue'
import { AnalyticsSettings, SetAnalyticsEnabled } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'

const PRIVACY_URL = 'https://hivedesktop.com/configuration/privacy/'
const enabled = ref(true)
const configured = ref(false)
const overridden = ref(false)
const loading = ref(true)
const error = ref('')

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const current = await AnalyticsSettings()
    enabled.value = current.enabled
    configured.value = current.configured
    overridden.value = current.overridden
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading.value = false
  }
}

async function setEnabled(value: boolean): Promise<void> {
  const previous = enabled.value
  enabled.value = value
  error.value = ''
  try {
    const current = await SetAnalyticsEnabled(value)
    enabled.value = current.enabled
    configured.value = current.configured
    overridden.value = current.overridden
  } catch (cause) {
    enabled.value = previous
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}

onMounted(() => { void refresh() })
</script>

<template>
  <SettingsPage testid="settings-analytics">
    <SettingsError v-if="error" :message="error" testid="analytics-error" />

    <SettingsSection
      title="Anonymous usage data"
      description="Help measure Hive Desktop adoption without sending account or workspace data."
      boxed
    >
      <SettingsRow
        label="Share daily activity"
        hint="Sends at most one event per UTC day while Hive is running. Turning this off takes effect immediately."
        testid="analytics-enabled-row"
      >
        <div class="flex items-center gap-3">
          <BaseBadge v-if="!loading && overridden" tone="neutral" variant="pill" class="px-2.5 py-1 text-[11px] font-semibold" data-testid="analytics-overridden">
            Environment override
          </BaseBadge>
          <BaseBadge v-if="!loading && !configured" tone="neutral" variant="pill" class="px-2.5 py-1 text-[11px] font-semibold" data-testid="analytics-unconfigured">
            Not configured
          </BaseBadge>
          <AppSwitch
            :model-value="enabled"
            :disabled="loading || overridden"
            aria-label="Share daily activity"
            testid="analytics-enabled"
            @update:model-value="setEnabled"
          />
        </div>
      </SettingsRow>
    </SettingsSection>

    <SettingsSection
      title="What Hive sends"
      description="A random installation ID, app version, release channel, operating system, and CPU architecture."
    >
      <p class="text-xs leading-relaxed text-text-3">
        Hive does not send names, email addresses, repository data, source events, file paths, flow contents, commands, or interaction events. PostHog person profiles and GeoIP enrichment are disabled.
      </p>
      <button
        type="button"
        class="mt-3 flex cursor-pointer items-center gap-1.5 text-[12px] font-medium text-accent hover:underline"
        data-testid="analytics-privacy"
        @click="Browser.OpenURL(PRIVACY_URL)"
      ><IconBookOpen class="size-3.5" />Privacy details ↗</button>
    </SettingsSection>
  </SettingsPage>
</template>
