import { useAgentWorkspaces } from './useAgentWorkspaces'
import type {
  AgentSchedule,
  AgentSchedulePreview,
  AgentScheduleRun,
  SchedulePreviewRequest,
} from '../lib/agentWorkspacesClient'

// The schedule calls that are not part of saving a workspace, resolved against
// the same client every other Chats list uses. There is deliberately no shared
// state here any more: a schedule's definition rides the workspace view
// (AgentWorkspace.schedules), so the editor holds the only copy that is being
// edited and the sidebar reads the saved one from the workspace list. A module
// singleton would have been a third copy of rows both of those already have,
// and two editors open on different workspaces would have fought over it.

/** How much of a schedule's history the editor's disclosure asks for. */
export const RUN_HISTORY_LIMIT = 20

async function resolveClient() {
  const { client, ready } = useAgentWorkspaces()
  await ready()
  if (!client.value) throw new Error('The Chats area is unavailable.')
  return client.value
}

export function useAgentSchedules(): {
  /** A workspace's schedules with fresh nextRunAt/lastRun: the refresh after a manual run. */
  list: (workspace: string) => Promise<AgentSchedule[]>
  runs: (workspace: string, id: string, limit?: number) => Promise<AgentScheduleRun[]>
  runNow: (workspace: string, id: string) => Promise<AgentScheduleRun>
  preview: (request: SchedulePreviewRequest) => Promise<AgentSchedulePreview>
} {
  return {
    async list(workspace) {
      return await (await resolveClient()).schedules(workspace)
    },
    async runs(workspace, id, limit = RUN_HISTORY_LIMIT) {
      return await (await resolveClient()).scheduleRuns(workspace, id, limit)
    },
    async runNow(workspace, id) {
      return await (await resolveClient()).runSchedule(workspace, id)
    },
    async preview(request) {
      return await (await resolveClient()).previewSchedule(request)
    },
  }
}
