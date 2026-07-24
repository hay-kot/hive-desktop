<script setup lang="ts">
import { computed, onMounted } from 'vue'
import AppSwitch from './AppSwitch.vue'
import SettingsSection from './settings/SettingsSection.vue'
import { useNotificationSettings, type NotificationDelivery } from '../composables/useNotificationSettings'

const {
  notificationsEnabled,
  delivery,
  notificationSound,
  permission,
  requestingPermission,
  error,
  refresh,
  setNotificationsEnabled,
  setDelivery,
  setNotificationSound,
  requestPermission,
} = useNotificationSettings()

const permissionLabel = computed(() => ({
  granted: 'Granted',
  denied: 'Denied',
  'not-requested': 'Not requested',
}[permission.value]))

const deliveryOptions: Array<{ value: NotificationDelivery; label: string; hint: string }> = [
  { value: 'auto', label: 'Automatic', hint: 'Show an OS banner only while you are working in another app.' },
  { value: 'system', label: 'Always a system banner', hint: 'Show an OS banner even when Hive is focused.' },
  { value: 'app', label: 'Always in Hive', hint: 'Never show an OS banner; notifications stay in the app.' },
]

onMounted(() => {
  // Permission can change in OS settings while this window is open, so every
  // visit refreshes both persisted preferences and the live authorization.
  void refresh()
})
</script>

<template>
  <!--
    Behavior matrix:
    master off: Activity only, whether Hive is focused or unfocused; delivery is disabled.
    master on + delivery "app": in-app toast only, focused or not.
    master on + delivery "auto" + Hive focused: in-app toast; no OS banner.
    master on + delivery "auto" + Hive unfocused: OS banner.
    master on + delivery "system": OS banner, focused or not.
    Sound applies only to OS banners; a flow's notify node can quiet itself but never unmute.
    OS permission denied/not requested: preferences persist, but a banner falls back to a toast.
  -->
  <div class="mx-auto max-w-[640px]" data-testid="notification-settings">
    <div
      v-if="error"
      class="mb-4 rounded-md border border-border bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
      data-testid="notification-settings-error"
    >{{ error }}</div>

    <SettingsSection
      title="Notifications"
      description="Choose how Hive keeps you informed about new activity."
      class="[&>p]:mb-3"
    >
      <div class="rounded-lg border border-border">
        <div class="px-3.5 py-3">
          <AppSwitch
            :model-value="notificationsEnabled"
            label="Enable notifications"
            hint="Show Hive notifications for activity that needs your attention."
            testid="notification-enable"
            @update:model-value="setNotificationsEnabled"
          />
        </div>
        <div class="border-t border-border px-3.5 py-3" :class="{ 'opacity-50': !notificationsEnabled }">
          <div class="text-[12.5px] font-medium text-text">Delivery</div>
          <p class="mt-0.5 text-[12px] text-text-3">Where a notification shows up when one is raised.</p>
          <div class="mt-2.5 flex flex-col gap-2" data-testid="notification-delivery">
            <label
              v-for="option in deliveryOptions"
              :key="option.value"
              class="flex cursor-pointer items-start gap-2.5"
              :class="{ 'cursor-not-allowed': !notificationsEnabled }"
            >
              <input
                type="radio"
                class="mt-0.5 accent-accent"
                name="notification-delivery"
                :value="option.value"
                :checked="delivery === option.value"
                :disabled="!notificationsEnabled"
                :data-testid="`notification-delivery-${option.value}`"
                @change="setDelivery(option.value)"
              >
              <span>
                <span class="block text-[12.5px] text-text">{{ option.label }}</span>
                <span class="block text-[12px] text-text-3">{{ option.hint }}</span>
              </span>
            </label>
          </div>
        </div>
        <div class="border-t border-border px-3.5 py-3">
          <AppSwitch
            :model-value="notificationSound"
            label="Play sound"
            hint="Play sound for OS banners only."
            testid="notification-sound"
            @update:model-value="setNotificationSound"
          />
        </div>
      </div>
    </SettingsSection>

    <SettingsSection
      title="System permission"
      description="Hive needs operating-system permission before it can show notification banners."
      class="mt-6 [&>p]:mb-3"
    >
      <div class="rounded-lg border border-border px-3.5 py-3">
        <div class="flex items-center justify-between gap-3">
          <span class="text-[12.5px] text-text-3">OS notification permission</span>
          <span class="text-[12.5px] font-medium text-text" data-testid="notification-permission-status">{{ permissionLabel }}</span>
        </div>
        <div v-if="permission === 'not-requested'" class="mt-3">
          <button
            type="button"
            class="cursor-pointer rounded-md border border-border px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:bg-chip hover:text-text disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="requestingPermission"
            data-testid="notification-permission-request"
            @click="requestPermission"
          >{{ requestingPermission ? 'Requesting…' : 'Allow notifications' }}</button>
        </div>
        <p v-else-if="permission === 'denied'" class="mt-3 text-xs leading-relaxed text-text-3" data-testid="notification-permission-denied-guidance">
          Notifications are blocked. Enable them for Hive in your operating system's notification settings.
        </p>
      </div>
    </SettingsSection>
  </div>
</template>
