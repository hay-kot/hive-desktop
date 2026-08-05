<script setup lang="ts">
// Agents settings: the opt-in that gates the Agents area and where its
// workspaces live. Everything a workspace itself declares — agent, autonomy
// posture, MCP servers — belongs to the workspace manifest, not here.
import { onMounted } from 'vue'
import SettingsError from './settings/SettingsError.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsPathRow from './settings/SettingsPathRow.vue'
import SettingsRow from './settings/SettingsRow.vue'
import SettingsSection from './settings/SettingsSection.vue'
import ExperimentalToggle from './settings/ExperimentalToggle.vue'
import { useExperimentalSettings } from '../composables/useExperimentalSettings'
import { useSystemSettings } from '../composables/useSystemSettings'

const { agents, agentsRestartPending, setAgents, error: experimentalError, refresh: refreshExperimental } = useExperimentalSettings()
const { info, error: locationsError, refresh: refreshLocations, openPath, revealPath } = useSystemSettings()

onMounted(() => {
  void refreshExperimental()
  void refreshLocations()
})
</script>

<template>
  <SettingsPage testid="settings-agents">
    <SettingsError v-if="experimentalError" :message="experimentalError" testid="agents-error" />
    <SettingsError v-else-if="locationsError" :message="locationsError" testid="agents-error" />

    <SettingsSection
      title="Agents area"
      description="Off by default; changes apply after restarting Hive."
      boxed
      testid="agents-mode"
    >
      <SettingsRow
        label="Agents area"
        hint="Run a CLI agent against a named workspace with its own MCP tool set, from the mode switch in the title bar. A workspace declares its own autonomy posture — nothing here inherits a coding session's flags."
      >
        <ExperimentalToggle
          :model-value="agents"
          :restart-pending="agentsRestartPending"
          ariaLabel="Agents area"
          testid="agents-mode-enabled"
          @update:model-value="setAgents"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection
      v-if="info"
      title="Workspaces"
      description="Where the Agents area keeps its workspace directories."
      boxed
    >
      <SettingsPathRow
        label="Workspace root"
        hint="Set agent_workspaces.dir in settings.yaml to move it — iCloud Drive and other synced folders are expected destinations. A new location applies after restart."
        icon="folder"
        tone="accent"
        :path="info.agentWorkspaces.path"
        :exists="info.agentWorkspaces.exists"
        testid="agents-workspace-root"
        @open="openPath(info.agentWorkspaces.path)"
        @reveal="revealPath(info.agentWorkspaces.path)"
      />
    </SettingsSection>
  </SettingsPage>
</template>
