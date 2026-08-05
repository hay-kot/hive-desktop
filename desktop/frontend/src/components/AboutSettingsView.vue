<script setup lang="ts">
// About: which build is running, where it came from, and the auto-update
// controls that replace it. Not a settings group — it is the answer to "what
// am I running", which is why it is its own pane rather than a card at the
// bottom of System.
import { onMounted } from 'vue'
import IconExternalLink from '~icons/lucide/external-link'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import AppSwitch from './AppSwitch.vue'
import SettingsError from './settings/SettingsError.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsSection from './settings/SettingsSection.vue'
import { useAboutSettings } from '../composables/useAboutSettings'

const {
  build,
  update,
  autoUpdate,
  checking,
  checkedOnce,
  error,
  refresh,
  setAutoUpdate,
  checkForUpdates,
  openReleaseNotes,
  openRepo,
} = useAboutSettings()

onMounted(() => {
  void refresh()
})
</script>

<template>
  <SettingsPage testid="settings-about">
    <SettingsError v-if="error" :message="error" testid="about-error" />

    <SettingsSection
      v-if="build"
      title="Build"
      description="The build of Hive you're running — include this when reporting an issue."
      boxed
      testid="about-build"
    >
      <div class="flex flex-wrap items-center gap-x-6 gap-y-3 px-4 py-3.5">
        <div class="flex flex-col gap-1">
          <span class="text-[11px] text-text-3">Version</span>
          <div class="flex items-center gap-2">
            <span class="font-mono text-[13px] text-text" data-testid="about-build-version">{{ build.version }}</span>
            <span
              v-if="update?.available"
              class="rounded-full border border-severity-info-border bg-severity-info-tint px-2 py-0.5 text-[11px] font-medium text-severity-info"
              data-testid="about-update-available"
            >Update available: {{ update.latestVersion }}</span>
            <span
              v-else-if="checkedOnce"
              class="text-[11px] text-text-4"
              data-testid="about-update-uptodate"
            >Up to date</span>
          </div>
        </div>
        <div class="hidden h-[26px] w-px bg-row @[520px]/pane:block" />
        <div class="flex flex-col gap-1">
          <span class="text-[11px] text-text-3">Commit</span>
          <span class="font-mono text-[13px] text-text" data-testid="about-build-commit">{{ build.commit }}</span>
        </div>
        <div class="hidden h-[26px] w-px bg-row @[520px]/pane:block" />
        <div class="flex flex-col gap-1">
          <span class="text-[11px] text-text-3">Built</span>
          <span class="font-mono text-[13px] text-text" data-testid="about-build-date">{{ build.date }}</span>
        </div>
        <div class="flex-1" />
        <div class="flex items-center gap-4 text-[13px]">
          <button
            type="button"
            class="flex cursor-pointer items-center gap-1.5 text-accent hover:underline"
            title="View project on GitHub"
            data-testid="about-build-repo"
            @click="openRepo"
          ><IconExternalLink class="size-3.5" />Project</button>
          <button
            v-if="build.releaseUrl"
            type="button"
            class="flex cursor-pointer items-center gap-1.5 text-accent hover:underline"
            title="View release on GitHub"
            data-testid="about-build-release"
            @click="openReleaseNotes"
          ><IconExternalLink class="size-3.5" />Release</button>
        </div>
      </div>
      <div class="flex flex-col gap-3 border-t border-row px-4 py-3.5 @[600px]/pane:flex-row @[600px]/pane:items-center @[600px]/pane:gap-3.5">
        <div class="flex min-w-0 flex-1 items-center gap-3.5">
          <AppSwitch
            :model-value="autoUpdate"
            aria-label="Automatic updates"
            testid="about-auto-update"
            @update:model-value="setAutoUpdate"
          />
          <div class="min-w-0 flex-1">
            <div class="text-[13.5px] font-semibold text-text">Automatic updates</div>
            <div class="mt-0.5 text-[11.5px] text-text-3">Check for and install new versions from GitHub in the background.</div>
          </div>
        </div>
        <button
          type="button"
          class="flex shrink-0 cursor-pointer items-center gap-1.5 self-end rounded-[7px] border border-card px-3 py-1.5 text-[12.5px] font-medium text-text-2 hover:border-strong hover:text-text disabled:cursor-not-allowed disabled:opacity-50 @[600px]/pane:self-auto"
          :disabled="checking"
          data-testid="about-check-update"
          @click="checkForUpdates"
        >
          <IconRefreshCw class="size-3.5" :class="checking ? 'animate-spin' : ''" />
          {{ checking ? 'Checking…' : 'Check for updates' }}
        </button>
      </div>
    </SettingsSection>
  </SettingsPage>
</template>
