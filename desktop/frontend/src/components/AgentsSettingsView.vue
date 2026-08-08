<script setup lang="ts">
// Agents settings: where the area's workspaces live. Everything a workspace
// itself declares — agent, autonomy posture, MCP servers — belongs to the
// workspace manifest, not here.
import { onMounted } from 'vue'
import SettingsError from './settings/SettingsError.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsPathRow from './settings/SettingsPathRow.vue'
import SettingsSection from './settings/SettingsSection.vue'
import { useSystemSettings } from '../composables/useSystemSettings'

const { info, error: locationsError, refresh: refreshLocations, openPath, revealPath } = useSystemSettings()

onMounted(() => {
  void refreshLocations()
})
</script>

<template>
  <SettingsPage testid="settings-agents">
    <SettingsError v-if="locationsError" :message="locationsError" testid="agents-error" />

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
