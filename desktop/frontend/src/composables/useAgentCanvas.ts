import { ref, shallowRef, watch } from 'vue'
import type { Ref, ShallowRef } from 'vue'
import type { AgentWorkspacesClient, ChatCanvas, ChatCanvasMeta } from '../lib/agentWorkspacesClient'

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

/**
 * One canvas pane's state: the canvas being shown plus the workspace's canvas
 * listing for the picker. Per-pane, not a module singleton — the pane owns the
 * lifetime. The generation guard is useActionsSettings' shape: a canvas:updated
 * wake and a session switch can race, and an older response must never replace
 * a newer request's.
 */
export function useAgentCanvas(client: Ref<AgentWorkspacesClient | null>) {
  const canvas: ShallowRef<ChatCanvas | null> = shallowRef(null)
  const metas: Ref<ChatCanvasMeta[]> = ref([])
  const session: Ref<number | null> = ref(null)
  const workspace = ref('')
  const loading = ref(false)
  const error = ref('')
  let generation = 0
  let queued = false
  let running = false

  async function reload(): Promise<void> {
    if (running) { generation++; queued = true; return }
    running = true
    const token = ++generation
    const id = session.value
    const dir = workspace.value
    loading.value = true
    try {
      const [loaded, listed] = await Promise.all([
        id !== null && client.value ? client.value.canvas(id) : Promise.resolve(null),
        dir && client.value ? client.value.canvases(dir) : Promise.resolve([]),
      ])
      if (token === generation) {
        canvas.value = loaded
        metas.value = listed
        error.value = ''
      }
    } catch (err) {
      if (token === generation) {
        canvas.value = null
        error.value = message(err, 'Could not read the canvas.')
      }
    } finally {
      if (token === generation) loading.value = false
      running = false
      if (queued) { queued = false; void reload() }
    }
  }

  function wake(): void {
    generation++
    if (running) queued = true
    else void reload()
  }

  /** Points the pane at one session's canvas within its workspace's listing. */
  function show(id: number | null, dir: string): void {
    session.value = id
    workspace.value = dir
    wake()
  }

  watch(client, () => wake())

  return { canvas, metas, session, workspace, loading, error, show, wake }
}
