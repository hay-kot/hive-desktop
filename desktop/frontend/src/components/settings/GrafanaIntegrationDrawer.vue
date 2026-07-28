<script setup lang="ts">
import { computed, ref } from 'vue'
import IconActivity from '~icons/lucide/activity'
import BaseButton from '../BaseButton.vue'
import DrawerSheet from '../DrawerSheet.vue'
import SettingsField from './SettingsField.vue'
import { useGrafanaConnection } from '../../composables/useGrafanaConnection'
import { useIntegrations } from '../../composables/useIntegrations'

const emit = defineEmits<{ close: [] }>()

// Acquisition is provider-specific, so it lives here rather than on the generic
// Integrations card. The connected accounts come from the same registry
// projection the card reads, which re-reads on the connection:updated signal a
// connect or disconnect publishes.
const { busy, error, connect, disconnect } = useGrafanaConnection()
const { accountsFor } = useIntegrations()
const connectedAccounts = computed(() => accountsFor('grafana'))

const urlInput = ref('')
const tokenInput = ref('')
const disconnecting = ref<string | null>(null)

const canConnect = computed(() => urlInput.value.trim() !== '' && tokenInput.value.trim() !== '')

async function onConnect() {
  if (!canConnect.value) return
  const ok = await connect(urlInput.value.trim(), tokenInput.value.trim())
  if (ok) {
    urlInput.value = ''
    tokenInput.value = ''
  }
}

async function onDisconnect(account: string) {
  disconnecting.value = account
  try {
    await disconnect(account)
  } finally {
    disconnecting.value = null
  }
}
</script>

<template>
  <DrawerSheet ariaLabel="Grafana settings" testid="grafana-integration-drawer" backdrop-testid="grafana-integration-backdrop" :default-size="380" :min="320" :max="560" @close="emit('close')">
    <template #header>
      <div class="flex items-center gap-2.5">
        <span class="flex size-[26px] items-center justify-center rounded-[7px] bg-chip text-text-2"><IconActivity class="size-3.5" /></span>
        <div>
          <div class="text-[14px] font-semibold tracking-[-.01em]">Grafana settings</div>
          <div class="font-mono text-[11px] text-text-3">Connected stacks</div>
        </div>
      </div>
    </template>

    <SettingsField label="Connected stacks" testid="grafana-connected">
      <div v-if="connectedAccounts.length > 0" class="flex flex-col gap-2">
        <div
          v-for="account in connectedAccounts"
          :key="account"
          class="flex items-center justify-between gap-3 rounded-lg border border-border bg-raised px-3 py-2.5"
          :data-testid="`grafana-connected-${account}`"
        >
          <div class="min-w-0 truncate font-mono text-[13px] text-text">{{ account }}</div>
          <BaseButton
            variant="secondary"
            size="sm"
            :busy="disconnecting === account"
            :data-testid="`grafana-disconnect-${account}`"
            @click="onDisconnect(account)"
          >Disconnect</BaseButton>
        </div>
      </div>
      <div v-else class="rounded-lg border border-border bg-raised px-3 py-2.5 text-[13px] text-text-3" data-testid="grafana-connected-empty">
        No stack connected
      </div>
    </SettingsField>

    <div class="mt-5">
      <SettingsField label="Connect a stack" hint="Paste the stack URL and a service-account token. The token is validated once and stored in your keychain; only the URL is written to disk." testid="grafana-connect">
        <div class="flex flex-col gap-2">
          <input
            v-model="urlInput"
            type="url"
            placeholder="https://grafana.example.com"
            class="w-full rounded-lg border border-strong bg-app px-[11px] py-[9px] font-mono text-[13px] text-text outline-none focus:border-accent"
            data-testid="grafana-connect-url"
          >
          <input
            v-model="tokenInput"
            type="password"
            placeholder="glsa_…"
            class="w-full rounded-lg border border-strong bg-app px-[11px] py-[9px] font-mono text-[13px] text-text outline-none focus:border-accent"
            data-testid="grafana-connect-token"
          >
          <div>
            <BaseButton size="sm" :busy="busy" :disabled="!canConnect" data-testid="grafana-connect-submit" @click="onConnect">Connect</BaseButton>
          </div>
        </div>
      </SettingsField>
    </div>
    <p v-if="error" class="mt-2 text-xs text-severity-error" data-testid="grafana-connect-error">{{ error }}</p>

    <template #footer>
      <div class="flex items-center justify-end gap-2.5">
        <BaseButton variant="secondary" size="sm" data-testid="grafana-settings-close" @click="emit('close')">Close</BaseButton>
      </div>
    </template>
  </DrawerSheet>
</template>
