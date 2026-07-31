import { useStorage } from '@vueuse/core'
import { ref, shallowRef, type Ref, type ShallowRef } from 'vue'
import { createTerminalClient, type TerminalClient, type TerminalEndpoint, type TerminalEngine } from '../lib/terminalClient'

// The availability probe's answer and the control client, cached across
// terminal-mode visits: the component unmounts on every trip to the hub, and
// re-probing from scratch blanked the view behind the "Checking tmux…" gate.
// Re-entry renders the last-known answer immediately while the mount re-probes
// in the background. The endpoint (loopback address + bearer token) is stable
// for the life of the backend process, so a created client is never re-minted.
const checking = ref(true)
const available = ref(false)
const reason = ref('')
const ptyAvailable = ref(false)
const client: ShallowRef<TerminalClient | null> = shallowRef(null)

// Which backend the view drives (ADR 0045). Remembered so a comparison run
// survives leaving terminal mode, and reset to tmux when the process-managed
// backend turns out not to be mounted — a remembered engine must not strand the
// view on a surface that is not there.
const engine = useStorage<TerminalEngine>('hive.terminal.engine', 'tmux')

let endpoint: TerminalEndpoint | null = null

export function useTerminalAvailability(): {
  checking: Ref<boolean>
  available: Ref<boolean>
  reason: Ref<string>
  ptyAvailable: Ref<boolean>
  engine: Ref<TerminalEngine>
  client: ShallowRef<TerminalClient | null>
  openTransport: (resolved: TerminalEndpoint) => void
  useEngine: (next: TerminalEngine) => void
} {
  return { checking, available, reason, ptyAvailable, engine, client, openTransport, useEngine }
}

/** Caches the endpoint and builds the client for whichever engine is selected. */
function openTransport(resolved: TerminalEndpoint): void {
  endpoint = resolved
  if (!resolved.ptyWSURL) {
    ptyAvailable.value = false
    engine.value = 'tmux'
  }
  client.value = createTerminalClient(resolved, engine.value)
}

/**
 * Switches backends. The client is replaced rather than reconfigured: window
 * ids, sessions and streams belong to one engine, so nothing an old client
 * opened is meaningful to the new one and the caller drops its pool.
 */
function useEngine(next: TerminalEngine): void {
  if (next === engine.value || !endpoint) return
  if (next === 'pty' && !endpoint.ptyWSURL) return
  engine.value = next
  client.value = createTerminalClient(endpoint, next)
}

export function resetTerminalAvailabilityForTests(): void {
  checking.value = true
  available.value = false
  reason.value = ''
  ptyAvailable.value = false
  engine.value = 'tmux'
  client.value = null
  endpoint = null
}
