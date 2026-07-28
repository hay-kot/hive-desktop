import { ref } from 'vue'
import { Connect, Disconnect } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/grafanaservice'
import type { Stack } from '../types/grafana'

/**
 * The Grafana connector's acquisition, as the settings drawer drives it.
 *
 * Unlike GitHub there is no device flow and no polled status: a stack is
 * connected by pasting its URL and a service-account token, validated once. The
 * connected-accounts list the drawer shows comes from useIntegrations, which
 * re-reads on the connection:updated signal a connect or disconnect publishes —
 * so this composable owns only the in-flight connect/disconnect state.
 */
export function useGrafanaConnection() {
  const busy = ref(false)
  const error = ref<string | null>(null)
  // The stack the last successful connect resolved to, so the drawer can
  // confirm which account a paste landed as.
  const lastConnected = ref<Stack | null>(null)

  async function connect(url: string, token: string): Promise<boolean> {
    if (busy.value) return false
    error.value = null
    busy.value = true
    try {
      lastConnected.value = await Connect(url, token)
      return true
    } catch (err) {
      error.value = messageOf(err, 'Grafana rejected the connection.')
      return false
    } finally {
      busy.value = false
    }
  }

  async function disconnect(account: string): Promise<void> {
    error.value = null
    try {
      await Disconnect(account)
    } catch (err) {
      error.value = messageOf(err, 'Could not disconnect the stack.')
    }
  }

  return { busy, error, lastConnected, connect, disconnect }
}

function messageOf(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) return err.message
  if (typeof err === 'string' && err) return err
  return fallback
}
