<script setup lang="ts">
// System settings: on-disk locations (data dir, config dir, log file,
// database) grouped into one card per section with per-row open/reveal
// actions, point-only overrides for the data and config directories that take
// effect after a restart, and an About card with the build strip and
// auto-update controls.
//
// The restart banner is a projection of SettingsService.RestartPending, so a
// startup-only setting added later surfaces here without this view learning
// about it.
import { onMounted } from 'vue'
import IconInfo from '~icons/lucide/info'
import IconExternalLink from '~icons/lucide/external-link'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconBug from '~icons/lucide/bug'
import IconChevronRight from '~icons/lucide/chevron-right'
import SettingsPathRow from './settings/SettingsPathRow.vue'
import AppSwitch from './AppSwitch.vue'
import { useSystemSettings } from '../composables/useSystemSettings'
import { useReportDialog } from '../composables/useReportDialog'

const { openDialog: openReport } = useReportDialog()

const {
  info,
  build,
  error,
  restartRequired,
  restartPending,
  autoUpdate,
  update,
  checkingUpdate,
  checkedOnce,
  experimentalTerminal,
  terminalRestartPending,
  setExperimentalTerminal,
  setAutoUpdate,
  checkForUpdates,
  refresh,
  openReleaseNotes,
  openRepo,
  openPath,
  revealPath,
  changeDataDir,
  changeConfigDir,
  resetDataDir,
  resetConfigDir,
  quit,
} = useSystemSettings()

onMounted(() => {
  void refresh()
})
</script>

<template>
  <div class="mx-auto flex max-w-[860px] flex-col gap-6" data-testid="settings-system">
    <div
      v-if="restartRequired"
      class="flex items-center gap-3 rounded-lg border border-border bg-severity-info-tint p-3.5"
      data-testid="system-restart-banner"
    >
      <IconInfo class="size-4 shrink-0 text-severity-info" />
      <div class="min-w-0 flex-1 text-[12.5px] text-text-2">
        <p>These take effect after restarting Hive:</p>
        <ul class="mt-1 flex flex-col gap-0.5">
          <li v-for="field in restartPending" :key="field.field" class="text-[11.5px] text-text-3">
            <span class="font-mono text-text-2">{{ field.field }}</span> — {{ field.reason }}
          </li>
        </ul>
      </div>
      <button
        type="button"
        class="shrink-0 cursor-pointer rounded-md border border-border px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:bg-chip hover:text-text"
        data-testid="system-quit"
        @click="quit"
      >Quit Hive</button>
    </div>

    <div
      v-if="error"
      class="rounded-md border border-border bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
      data-testid="system-error"
    >{{ error }}</div>

    <button
      type="button"
      class="group flex cursor-pointer items-center gap-3.5 overflow-hidden rounded-[11px] border border-accent/40 bg-raised px-4 py-3.5 text-left transition-colors hover:border-accent hover:bg-accent-tint/20"
      data-testid="system-report-problem"
      @click="openReport"
    >
      <span class="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent">
        <IconBug class="size-[18px]" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="text-[13.5px] font-semibold text-text">Report a problem</div>
        <div class="mt-0.5 text-[11.5px] text-text-3">Send build info, recent logs, and redacted config — secrets are removed first.</div>
      </div>
      <IconChevronRight class="size-4 shrink-0 text-text-3 transition-transform group-hover:translate-x-0.5 group-hover:text-text-2" />
    </button>

    <section class="flex flex-col gap-2.5">
      <div class="flex flex-wrap items-baseline gap-x-2.5 gap-y-1">
        <h2 class="text-xs font-semibold uppercase tracking-[.1em] text-text-2">Storage locations</h2>
        <p class="text-xs text-text-3">Point Hive at a different folder. Existing data isn't moved; a new location applies after restart.</p>
      </div>
      <div v-if="info" class="divide-y divide-row overflow-hidden rounded-[11px] border border-card bg-raised">
        <SettingsPathRow
          label="Data directory"
          hint="Desktop state, logs, and the databases live here."
          icon="folder"
          tone="accent"
          :path="info.dataDir.path"
          :exists="info.dataDir.exists"
          :overridden="info.dataDir.overridden"
          editable
          testid="system-data-dir"
          @open="openPath(info.dataDir.path)"
          @reveal="revealPath(info.dataDir.path)"
          @change="changeDataDir"
          @reset="resetDataDir"
        />
        <SettingsPathRow
          label="Config directory"
          hint="Profiles, flows, and actions.yml."
          icon="folder"
          tone="accent"
          :path="info.configDir.path"
          :exists="info.configDir.exists"
          :overridden="info.configDir.overridden"
          editable
          testid="system-config-dir"
          @open="openPath(info.configDir.path)"
          @reveal="revealPath(info.configDir.path)"
          @change="changeConfigDir"
          @reset="resetConfigDir"
        />
      </div>
    </section>

    <section class="flex flex-col gap-2.5">
      <div class="flex flex-wrap items-baseline gap-x-2.5 gap-y-1">
        <h2 class="text-xs font-semibold uppercase tracking-[.1em] text-text-2">Diagnostics</h2>
        <p class="text-xs text-text-3">Open or locate the log file and database when troubleshooting.</p>
      </div>
      <div v-if="info" class="divide-y divide-row overflow-hidden rounded-[11px] border border-card bg-raised">
        <SettingsPathRow
          label="Log file"
          icon="log"
          :path="info.logFile.path"
          :exists="info.logFile.exists"
          testid="system-log-file"
          @open="openPath(info.logFile.path)"
          @reveal="revealPath(info.logFile.path)"
        />
        <SettingsPathRow
          label="Database"
          icon="database"
          :path="info.database.path"
          :exists="info.database.exists"
          testid="system-database"
          @open="openPath(info.database.path)"
          @reveal="revealPath(info.database.path)"
        />
      </div>
    </section>

    <section class="flex flex-col gap-2.5" data-testid="system-experimental">
      <div class="flex flex-wrap items-baseline gap-x-2.5 gap-y-1">
        <h2 class="text-xs font-semibold uppercase tracking-[.1em] text-text-2">Experimental</h2>
        <p class="text-xs text-text-3">Early features that ship off by default. Changes apply after restarting Hive.</p>
      </div>
      <div class="overflow-hidden rounded-[11px] border border-card bg-raised">
        <div class="flex items-center gap-3.5 px-4 py-3.5">
          <AppSwitch
            :model-value="experimentalTerminal"
            aria-label="Terminal mode"
            testid="system-experimental-terminal"
            @update:model-value="setExperimentalTerminal"
          />
          <div class="min-w-0 flex-1">
            <div class="text-[13.5px] font-semibold text-text">Terminal mode</div>
            <div class="mt-0.5 text-[11.5px] text-text-3">Attach to a session's tmux windows inside the app, from the Hub | Terminal switch in the title bar. Needs tmux 3.2 or newer on your PATH; closing Hive leaves the tmux sessions running.</div>
          </div>
          <span
            v-if="terminalRestartPending"
            class="shrink-0 rounded-full border border-severity-info-border bg-severity-info-tint px-2 py-0.5 text-[11px] font-medium text-severity-info"
            data-testid="system-terminal-restart"
          >Restart to apply</span>
        </div>
      </div>
    </section>

    <section class="flex flex-col gap-2.5" data-testid="system-about">
      <div class="flex flex-wrap items-baseline gap-x-2.5 gap-y-1">
        <h2 class="text-xs font-semibold uppercase tracking-[.1em] text-text-2">About</h2>
        <p class="text-xs text-text-3">The build of Hive you're running — include this when reporting an issue.</p>
      </div>
      <div v-if="build" class="overflow-hidden rounded-[11px] border border-card bg-raised">
        <div class="flex flex-wrap items-center gap-x-6 gap-y-3 px-4 py-3.5">
          <div class="flex flex-col gap-1">
            <span class="text-[11px] text-text-3">Version</span>
            <div class="flex items-center gap-2">
              <span class="font-mono text-[13px] text-text" data-testid="system-build-version">{{ build.version }}</span>
              <span
                v-if="update?.available"
                class="rounded-full border border-severity-info-border bg-severity-info-tint px-2 py-0.5 text-[11px] font-medium text-severity-info"
                data-testid="system-update-available"
              >Update available: {{ update.latestVersion }}</span>
              <span
                v-else-if="checkedOnce"
                class="text-[11px] text-text-4"
                data-testid="system-update-uptodate"
              >Up to date</span>
            </div>
          </div>
          <div class="hidden h-[26px] w-px bg-row @[520px]/pane:block" />
          <div class="flex flex-col gap-1">
            <span class="text-[11px] text-text-3">Commit</span>
            <span class="font-mono text-[13px] text-text" data-testid="system-build-commit">{{ build.commit }}</span>
          </div>
          <div class="hidden h-[26px] w-px bg-row @[520px]/pane:block" />
          <div class="flex flex-col gap-1">
            <span class="text-[11px] text-text-3">Built</span>
            <span class="font-mono text-[13px] text-text" data-testid="system-build-date">{{ build.date }}</span>
          </div>
          <div class="flex-1" />
          <div class="flex items-center gap-4 text-[13px]">
            <button
              type="button"
              class="flex cursor-pointer items-center gap-1.5 text-accent hover:underline"
              title="View project on GitHub"
              data-testid="system-build-repo"
              @click="openRepo"
            ><IconExternalLink class="size-3.5" />Project</button>
            <button
              v-if="build.releaseUrl"
              type="button"
              class="flex cursor-pointer items-center gap-1.5 text-accent hover:underline"
              title="View release on GitHub"
              data-testid="system-build-release"
              @click="openReleaseNotes"
            ><IconExternalLink class="size-3.5" />Release</button>
          </div>
        </div>
        <div class="flex flex-col gap-3 border-t border-row px-4 py-3.5 @[600px]/pane:flex-row @[600px]/pane:items-center @[600px]/pane:gap-3.5">
          <div class="flex min-w-0 flex-1 items-center gap-3.5">
            <AppSwitch
              :model-value="autoUpdate"
              aria-label="Automatic updates"
              testid="system-auto-update"
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
            :disabled="checkingUpdate"
            data-testid="system-check-update"
            @click="checkForUpdates"
          >
            <IconRefreshCw class="size-3.5" :class="checkingUpdate ? 'animate-spin' : ''" />
            {{ checkingUpdate ? 'Checking…' : 'Check for updates' }}
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
