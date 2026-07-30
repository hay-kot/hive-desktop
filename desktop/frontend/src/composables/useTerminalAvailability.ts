import { ref, shallowRef, type Ref, type ShallowRef } from 'vue'
import type { TerminalClient } from '../lib/terminalClient'

// The tmux availability probe's answer and the control client, cached across
// terminal-mode visits: the component unmounts on every trip to the hub, and
// re-probing from scratch blanked the view behind the "Checking tmux…" gate.
// Re-entry renders the last-known answer immediately while the mount re-probes
// in the background. The endpoint (loopback address + bearer token) is stable
// for the life of the backend process, so a created client is never re-minted.
const checking = ref(true)
const available = ref(false)
const reason = ref('')
const client: ShallowRef<TerminalClient | null> = shallowRef(null)

export function useTerminalAvailability(): {
  checking: Ref<boolean>
  available: Ref<boolean>
  reason: Ref<string>
  client: ShallowRef<TerminalClient | null>
} {
  return { checking, available, reason, client }
}

export function resetTerminalAvailabilityForTests(): void {
  checking.value = true
  available.value = false
  reason.value = ''
  client.value = null
}
