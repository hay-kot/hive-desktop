import { computed, ref } from 'vue'
import {
  ExperimentalSettings as LoadExperimentalSettings,
  SetExperimentalAgents,
  SetExperimentalTerminal,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import { Enabled as TerminalModeEnabled } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import { Enabled as AgentsModeEnabled } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// useExperimentalSettings drives the ships-dark opt-ins that gate whole modes:
// terminal (ADR 0037) and agents (ADR 0061). Each is read once at startup, so a
// toggle tracks two values — what is persisted and what this run mounted — and
// they differ exactly while a relaunch is pending.
//
// The two live together because the shape is identical, but each mode's switch
// is rendered on that mode's own settings pane: experimental is a posture a
// feature holds, not a category of settings.
export function useExperimentalSettings() {
  const error = ref('')

  const terminal = ref(false)
  const terminalRunning = ref(false)
  const terminalRestartPending = computed(() => terminal.value !== terminalRunning.value)

  const agents = ref(false)
  const agentsRunning = ref(false)
  const agentsRestartPending = computed(() => agents.value !== agentsRunning.value)

  async function refresh(): Promise<void> {
    error.value = ''
    try {
      const [persisted, terminalEnabled, agentsEnabled] = await Promise.all([
        LoadExperimentalSettings(), TerminalModeEnabled(), AgentsModeEnabled(),
      ])
      terminal.value = persisted.terminal
      agents.value = persisted.agents
      terminalRunning.value = terminalEnabled
      agentsRunning.value = agentsEnabled
    } catch (err) {
      error.value = errText(err)
    }
  }

  // The stored value comes from the reply so a process env override cannot
  // drift the switch from what the backend resolved. On failure the previous
  // value is restored so it never drifts from the backend either.
  async function setTerminal(value: boolean): Promise<void> {
    const previous = terminal.value
    terminal.value = value
    error.value = ''
    try {
      terminal.value = (await SetExperimentalTerminal(value)).terminal
    } catch (err) {
      terminal.value = previous
      error.value = errText(err)
    }
  }

  async function setAgents(value: boolean): Promise<void> {
    const previous = agents.value
    agents.value = value
    error.value = ''
    try {
      agents.value = (await SetExperimentalAgents(value)).agents
    } catch (err) {
      agents.value = previous
      error.value = errText(err)
    }
  }

  return {
    error,
    terminal,
    terminalRestartPending,
    setTerminal,
    agents,
    agentsRestartPending,
    setAgents,
    refresh,
  }
}
