import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { useCommandPalette } from '../composables/useCommands'
import { resetFlowsSessionForTests, useFlowsSession } from '../pipeline/composables/useFlowsSession'
import { resetNotificationSettingsForTests } from '../composables/useNotificationSettings'
import { applicationSettingsSections, createAppRouter } from '../router'

const mocks = vi.hoisted(() => ({
  // flowsservice
  ListFlows: vi.fn(),
  GetFlow: vi.fn(),
  CreateFlow: vi.fn(),
  SeedStarterFlow: vi.fn(),
  RenameFlow: vi.fn(),
  SetFlowEnabled: vi.fn(),
  DeleteFlow: vi.fn(),
  GetLayout: vi.fn(),
  SaveFlow: vi.fn(),
  SaveLayout: vi.fn(),
  GetSidebar: vi.fn(),
  SaveSidebar: vi.fn(),
  // actionsservice
  ListActions: vi.fn(),
  CreateAction: vi.fn(),
  UpdateAction: vi.fn(),
  DeleteAction: vi.fn(),
  // pipelineservice
  ListInboxItemsByFeed: vi.fn(),
  ListArchivedInboxItemsByFeed: vi.fn(),
  ListInboxItemsTrash: vi.fn(),
  FeedCounts: vi.fn(),
  MarkInboxItemUnread: vi.fn(),
  ToggleInboxItemArchived: vi.fn(),
  ToggleInboxItemIgnored: vi.fn(),
  InboxItemEvents: vi.fn(),
  ActionRun: vi.fn(),
  SessionLaunchOptions: vi.fn(),
  CreateSession: vi.fn(),
  NewSessionDraft: vi.fn(),
  ActionViews: vi.fn(),
  InvokeAction: vi.fn(),
  NodeRuns: vi.fn(),
  // github connection service
  Status: vi.fn(),
  StartDeviceFlow: vi.fn(),
  CancelDeviceFlow: vi.fn(),
  SetToken: vi.fn(),
  Disconnect: vi.fn(),
  // updaterservice
  UpdaterStatus: vi.fn(),
  InstallUpdate: vi.fn(),
  // notification settings
  NotificationSettings: vi.fn(),
  SetNotificationSettings: vi.fn(),
  PermissionStatus: vi.fn(),
  RequestNotificationPermission: vi.fn(),
  Notify: vi.fn(),
  Focused: vi.fn(),
  ActivityList: vi.fn(),
  RecordActivity: vi.fn(),
  // runtime
  On: vi.fn(),
  Hide: vi.fn(),
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/flowsservice', () => ({
  ListFlows: mocks.ListFlows,
  GetFlow: mocks.GetFlow,
  CreateFlow: mocks.CreateFlow,
  SeedStarterFlow: mocks.SeedStarterFlow,
  RenameFlow: mocks.RenameFlow,
  SetFlowEnabled: mocks.SetFlowEnabled,
  DeleteFlow: mocks.DeleteFlow,
  GetLayout: mocks.GetLayout,
  SaveFlow: mocks.SaveFlow,
  SaveLayout: mocks.SaveLayout,
  GetSidebar: mocks.GetSidebar,
  SaveSidebar: mocks.SaveSidebar,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/actionsservice', () => ({
  ListActions: mocks.ListActions,
  CreateAction: mocks.CreateAction,
  UpdateAction: mocks.UpdateAction,
  DeleteAction: mocks.DeleteAction,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice', () => ({
  ListInboxItemsByFeed: mocks.ListInboxItemsByFeed,
  ListArchivedInboxItemsByFeed: mocks.ListArchivedInboxItemsByFeed,
  ListInboxItemsTrash: mocks.ListInboxItemsTrash,
  FeedCounts: mocks.FeedCounts,
  MarkInboxItemUnread: mocks.MarkInboxItemUnread,
  ToggleInboxItemArchived: mocks.ToggleInboxItemArchived,
  ToggleInboxItemIgnored: mocks.ToggleInboxItemIgnored,
  InboxItemEvents: mocks.InboxItemEvents,
  ActionRun: mocks.ActionRun,
  NewSessionDraft: mocks.NewSessionDraft,
  ActionViews: mocks.ActionViews,
  InvokeAction: mocks.InvokeAction,
  NodeRuns: mocks.NodeRuns,
}))
vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  SessionLaunchOptions: mocks.SessionLaunchOptions,
  CreateSession: mocks.CreateSession,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/githubservice', () => ({
  Status: mocks.Status,
  StartDeviceFlow: mocks.StartDeviceFlow,
  CancelDeviceFlow: mocks.CancelDeviceFlow,
  SetToken: mocks.SetToken,
  Disconnect: mocks.Disconnect,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/updaterservice', () => ({
  Status: mocks.UpdaterStatus,
  InstallUpdate: mocks.InstallUpdate,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  NotificationSettings: mocks.NotificationSettings,
  SetNotificationSettings: mocks.SetNotificationSettings,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/notificationservice', () => ({
  PermissionStatus: mocks.PermissionStatus,
  RequestNotificationPermission: mocks.RequestNotificationPermission,
  Notify: mocks.Notify,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/windowservice', () => ({ Focused: mocks.Focused }))
vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/activityservice', () => ({
  List: mocks.ActivityList,
  Record: mocks.RecordActivity,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.On },
  Window: { Hide: mocks.Hide },
  Call: { ByID: vi.fn() },
}))

const flow = {
  id: 'personal',
  name: 'Personal',
  enabled: true,
  nodes: [
    { id: 'src', type: 'sources.github' },
    { id: 'desktop', type: 'feed', name: 'Desktop UI' },
  ],
  wires: [{ from: 'src', to: 'desktop' }],
}

async function mountAppWithRouter() {
  const router = createAppRouter(createMemoryHistory())
  await router.push('/')
  await router.isReady()
  const wrapper = mount(App, { global: { plugins: [router] } })
  await flushPromises()
  return { wrapper, router }
}

async function mountApp() {
  return (await mountAppWithRouter()).wrapper
}

describe('App', () => {
  beforeEach(() => {
    // useFlowsSession is a module singleton (App.vue + FlowsView.vue share
    // one instance) — without this, a later test would silently reuse a
    // prior test's instance, including its already-torn-down onMounted/
    // watch hooks from that test's wrapper.unmount().
    resetFlowsSessionForTests()
    // useNotificationSettings is a module singleton too — reset it so a test's
    // resolved permission state cannot leak into the next test's first-run walk.
    resetNotificationSettingsForTests()
    vi.clearAllMocks()
    // Panel collapse / width state persists via useStorage; clear it so one
    // test's collapsed sidebar can't leak into the next.
    localStorage.clear()
    mocks.Status.mockResolvedValue({ state: 'connected', login: 'octocat', name: 'Octocat', avatarUrl: '', message: '' })
    mocks.ListFlows.mockResolvedValue([{ id: 'personal', name: 'Personal', enabled: true, valid: true }])
    mocks.GetFlow.mockResolvedValue(flow)
    mocks.GetLayout.mockResolvedValue({ nodes: {} })
    mocks.GetSidebar.mockResolvedValue({ items: [] })
    mocks.SaveSidebar.mockResolvedValue(undefined)
    mocks.ListInboxItemsByFeed.mockResolvedValue([])
    mocks.ListArchivedInboxItemsByFeed.mockResolvedValue([])
    mocks.ListInboxItemsTrash.mockResolvedValue([])
    mocks.FeedCounts.mockResolvedValue([{ feedId: 'personal/desktop', total: 1, unread: 0, archived: 0 }])
    mocks.InboxItemEvents.mockResolvedValue([])
    mocks.ActionRun.mockResolvedValue({ commandId: 1, status: 'done' })
    mocks.SessionLaunchOptions.mockResolvedValue({ repositories: [], defaultRepository: '', agents: [], defaultAgent: '' })
    mocks.ActionViews.mockResolvedValue([])
    mocks.InvokeAction.mockResolvedValue(undefined)
    mocks.ListActions.mockResolvedValue({ actions: [], error: '' })
    mocks.NodeRuns.mockResolvedValue([])
    mocks.RenameFlow.mockResolvedValue({ id: 'personal', name: 'Team', enabled: true, valid: true })
    mocks.SetFlowEnabled.mockImplementation(async (id: string, enabled: boolean) => ({ id, name: 'Personal', enabled, valid: true }))
    mocks.DeleteFlow.mockResolvedValue(undefined)
    mocks.On.mockReturnValue(() => {})
    mocks.UpdaterStatus.mockResolvedValue({ enabled: true, available: false, currentVersion: 'dev', latestVersion: '', notes: '', releaseUrl: '' })
    mocks.InstallUpdate.mockResolvedValue(undefined)
    mocks.NotificationSettings.mockResolvedValue({ notificationsEnabled: true, systemNotificationsEnabled: true, notificationSound: true })
    mocks.SetNotificationSettings.mockResolvedValue(undefined)
    mocks.PermissionStatus.mockResolvedValue('not-requested')
    mocks.RequestNotificationPermission.mockResolvedValue(true)
    mocks.Notify.mockResolvedValue(undefined)
    mocks.Focused.mockResolvedValue(true)
    mocks.ActivityList.mockResolvedValue([])
    mocks.RecordActivity.mockResolvedValue(undefined)
  })

  // ── First run ──────────────────────────────────────────────────────────────
  // create workspace -> connect GitHub -> feed. Nothing in the app is gated on
  // GitHub, so the only step that can hold the app back is having no workspace.

  it('opens the feed with GitHub disconnected — the app is not gated on it', async () => {
    mocks.Status.mockResolvedValue({ state: 'disconnected', login: '', name: '', avatarUrl: '', message: '' })
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Personal')

    wrapper.unmount()
  })

  it('walks first run: workspace first, then connect, which seeds the workspace it made', async () => {
    mocks.Status.mockResolvedValue({ state: 'disconnected', login: '', name: '', avatarUrl: '', message: '' })
    mocks.ListFlows.mockResolvedValue([])
    mocks.CreateFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true })
    mocks.SeedStarterFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true })
    const wrapper = await mountApp()

    // Step 1 is the workspace: it is the thing that exists without a credential.
    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-workspace-input"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-connect"]').exists()).toBe(false)

    mocks.ListFlows.mockResolvedValue([{ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true }])
    await wrapper.get('[data-testid="onboarding-workspace-input"]').setValue('Frontend Triage')
    await wrapper.get('[data-testid="onboarding-workspace-submit"]').trigger('click')
    await flushPromises()

    // Step 2 is connecting, and the app has not fallen through to the feed.
    expect(mocks.CreateFlow).toHaveBeenCalledWith('Frontend Triage')
    expect(wrapper.get('[data-testid="onboarding-connect"]').isVisible()).toBe(true)
    expect(mocks.SeedStarterFlow).not.toHaveBeenCalled()

    // The device-flow grant lands as connection:updated, not as a call result.
    mocks.Status.mockResolvedValue({ state: 'connected', login: 'octocat', name: 'Octocat', avatarUrl: '', message: '' })
    const connection = mocks.On.mock.calls.find(([event]) => event === 'connection:updated')?.[1] as ((ev: { data: string }) => void) | undefined
    expect(connection).toBeDefined()
    connection?.({ data: 'github' })
    await flushPromises()

    expect(mocks.SeedStarterFlow).toHaveBeenCalledWith('personal')

    // Step 3 is the notification grant — the last leg before the feed. Skipping
    // it lands on the feed just as granting would.
    expect(wrapper.get('[data-testid="onboarding-permissions-allow"]').isVisible()).toBe(true)
    await wrapper.get('[data-testid="onboarding-permissions-skip"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('asks for notification permission as the last first-run step, then grants and lands on the feed', async () => {
    mocks.Status.mockResolvedValue({ state: 'disconnected', login: '', name: '', avatarUrl: '', message: '' })
    mocks.ListFlows.mockResolvedValue([])
    mocks.CreateFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true })
    mocks.SeedStarterFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true })
    const wrapper = await mountApp()

    mocks.ListFlows.mockResolvedValue([{ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true }])
    await wrapper.get('[data-testid="onboarding-workspace-input"]').setValue('Frontend Triage')
    await wrapper.get('[data-testid="onboarding-workspace-submit"]').trigger('click')
    await flushPromises()

    mocks.Status.mockResolvedValue({ state: 'connected', login: 'octocat', name: 'Octocat', avatarUrl: '', message: '' })
    const connection = mocks.On.mock.calls.find(([event]) => event === 'connection:updated')?.[1] as ((ev: { data: string }) => void) | undefined
    connection?.({ data: 'github' })
    await flushPromises()

    // The permission prompt is asked here, deliberately — not lazily mid-usage.
    expect(wrapper.get('[data-testid="onboarding-permissions-allow"]').isVisible()).toBe(true)
    mocks.PermissionStatus.mockResolvedValue('granted')
    await wrapper.get('[data-testid="onboarding-permissions-allow"]').trigger('click')
    await flushPromises()
    expect(mocks.RequestNotificationPermission).toHaveBeenCalledOnce()

    await wrapper.get('[data-testid="onboarding-permissions-finish"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('skipping the connect step lands on a feed whose empty state points at Integrations', async () => {
    mocks.Status.mockResolvedValue({ state: 'disconnected', login: '', name: '', avatarUrl: '', message: '' })
    mocks.ListFlows.mockResolvedValue([])
    mocks.CreateFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true })
    // A workspace created before an account was connected has no graph at all.
    mocks.GetFlow.mockResolvedValue({ id: 'personal', name: 'Frontend Triage', enabled: true, nodes: [], wires: [] })
    const { wrapper, router } = await mountAppWithRouter()

    mocks.ListFlows.mockResolvedValue([{ id: 'personal', name: 'Frontend Triage', enabled: true, valid: true }])
    await wrapper.get('[data-testid="onboarding-workspace-input"]').setValue('Frontend Triage')
    await wrapper.get('[data-testid="onboarding-workspace-submit"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="onboarding-skip"]').trigger('click')
    await wrapper.get('[data-testid="onboarding-skip-confirm"]').trigger('click')
    await flushPromises()

    // Skipping connect advances to the notification grant; skip that too.
    await wrapper.get('[data-testid="onboarding-permissions-skip"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)
    expect(mocks.SeedStarterFlow).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-empty"]').text()).toContain('no account is connected')

    await wrapper.get('[data-testid="workspace-empty-integrations"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(router.currentRoute.value.params.section).toBe('integrations')

    wrapper.unmount()
  })

  it('stays on the feed when GitHub disconnects — Integrations is where that is repaired', async () => {
    const wrapper = await mountApp()
    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)

    mocks.Status.mockResolvedValue({ state: 'disconnected', login: '', name: '', avatarUrl: '', message: '' })
    const connection = mocks.On.mock.calls.find(([event]) => event === 'connection:updated')?.[1] as ((ev: { data: string }) => void) | undefined
    connection?.({ data: 'github' })
    await flushPromises()

    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Personal')

    wrapper.unmount()
  })

  it('confirms updates in-app and shows install failures', async () => {
    mocks.UpdaterStatus.mockResolvedValue({ enabled: true, available: true, currentVersion: '1.2.0', latestVersion: '1.3.0', notes: '', releaseUrl: '' })
    mocks.InstallUpdate.mockRejectedValue(new Error('checksum mismatch'))
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = await mountApp()

    try {
      await wrapper.get('[data-testid="titlebar-update-chip"]').trigger('click')
      expect(document.querySelector('[data-testid="update-confirmation"]')?.textContent).toContain('Download Hive 1.3.0')
      expect(mocks.InstallUpdate).not.toHaveBeenCalled()

      document.querySelector<HTMLButtonElement>('[data-testid="update-confirmation-confirm"]')?.click()
      await flushPromises()

      expect(mocks.InstallUpdate).toHaveBeenCalledOnce()
      expect(document.querySelector('[data-testid="update-confirmation-error"]')?.textContent).toContain('checksum mismatch')
      expect(wrapper.get('[data-testid="toast-title"]').text()).toBe('Could not install the update')
      expect(wrapper.get('[data-testid="toast-body"]').text()).toContain('checksum mismatch')
      expect(wrapper.get('[data-testid="titlebar-update-chip"]').attributes('disabled')).toBeUndefined()
    } finally {
      wrapper.unmount()
      consoleError.mockRestore()
    }
  })

  it('registers profile / feed-selection / flow-edit palette commands (not the removed feed-editor ones)', async () => {
    const wrapper = await mountApp()
    const { results, query } = useCommandPalette()
    query.value = ''

    const ids = results.value.map((cmd) => cmd.id)
    expect(ids).toContain('flow:edit')
    expect(ids).toContain('view:trash')
    expect(ids).toContain('feed:personal/desktop')
    expect(ids).toContain('profile:new')
    // Feed/source editing folded into the node drawer — these are gone.
    expect(ids).not.toContain('feed:new')
    expect(ids).not.toContain('feed:edit:desktop')
    expect(ids).not.toContain('feed:edit-config')

    wrapper.unmount()
  })

  it('opens the flows canvas from the sidebar and exits via the profile rail, keeping the rail mounted', async () => {
    const wrapper = await mountApp()

    // Feed view first: sidebar present, no flows canvas.
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    await wrapper.find('[data-testid="sidebar-edit-flow"]').trigger('click')
    await flushPromises()

    // Flows canvas is up; the spaces rail stays mounted as the way back.
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(true)
    const getFlowCallsBeforeExit = mocks.GetFlow.mock.calls.length

    await wrapper.find('[data-testid="profile-tile"][data-id="personal"]').trigger('click')
    await flushPromises()

    // Back to the feed view.
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)
    expect(mocks.GetFlow.mock.calls.length).toBeGreaterThan(getFlowCallsBeforeExit)

    wrapper.unmount()
  })

  it('renames the active profile from profile settings', async () => {
    const wrapper = await mountApp()

    await wrapper.get('[data-testid="sidebar-open-settings"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="profile-settings-name"]').setValue('Team')
    await wrapper.get('[data-testid="profile-settings-view"] form').trigger('submit')
    await flushPromises()

    expect(mocks.RenameFlow).toHaveBeenCalledWith('personal', 'Team')
    expect((wrapper.get('[data-testid="profile-settings-name"]').element as HTMLInputElement).value).toBe('Team')

    wrapper.unmount()
  })

  it('collapses and restores the feed sidebar from the title-bar toggle', async () => {
    const wrapper = await mountApp()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    await wrapper.find('[data-testid="titlebar-toggle-sidebar"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(false)

    await wrapper.find('[data-testid="titlebar-toggle-sidebar"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('collapses and restores the detail preview from the title-bar toggle and the p key', async () => {
    const wrapper = await mountApp()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(true)

    await wrapper.find('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)
    expect(localStorage.getItem('hive.panel.detailpane.collapsed')).toBe('true')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'p' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('opens profile settings from the sidebar gear and application settings from the rail', async () => {
    const wrapper = await mountApp()

    await wrapper.find('[data-testid="sidebar-open-settings"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="profile-settings-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="settings-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(false)

    await wrapper.find('[data-testid="profile-settings-close"]').trigger('click')
    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="profile-settings-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(true)

    await wrapper.find('[data-testid="settings-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('uses route history for settings pages and categories', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(wrapper.find('[data-testid="settings-theme-toggle-dark"]').exists()).toBe(true)

    await wrapper.find('[data-testid="settings-category-integrations"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.params.section).toBe('integrations')
    expect(wrapper.find('[data-testid="settings-integrations"]').exists()).toBe(true)

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(router.currentRoute.value.params.section).toBe('')
    expect(wrapper.find('[data-testid="settings-theme-toggle-dark"]').exists()).toBe(true)

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('feed')
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('uses mouse back and forward buttons for route history', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')

    const backDown = new MouseEvent('mousedown', { button: 3, cancelable: true })
    window.dispatchEvent(backDown)
    expect(backDown.defaultPrevented).toBe(true)

    const backUp = new MouseEvent('mouseup', { button: 3, cancelable: true })
    window.dispatchEvent(backUp)
    await flushPromises()
    expect(backUp.defaultPrevented).toBe(true)
    expect(router.currentRoute.value.name).toBe('feed')

    const forwardUp = new MouseEvent('mouseup', { button: 4, cancelable: true })
    window.dispatchEvent(forwardUp)
    await flushPromises()
    expect(forwardUp.defaultPrevented).toBe(true)
    expect(router.currentRoute.value.name).toBe('application-settings')

    wrapper.unmount()
  })

  it('suppresses Backspace history navigation outside editable fields', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()
    const routeBefore = router.currentRoute.value.fullPath

    const backspace = new KeyboardEvent('keydown', { key: 'Backspace', bubbles: true, cancelable: true })
    window.dispatchEvent(backspace)
    await flushPromises()

    expect(backspace.defaultPrevented).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe(routeBefore)

    wrapper.unmount()
  })

  it('allows Backspace to edit text inputs', async () => {
    const { wrapper } = await mountAppWithRouter()
    const search = wrapper.get('[data-testid="feed-search"]').element
    const backspace = new KeyboardEvent('keydown', { key: 'Backspace', bubbles: true, cancelable: true })

    search.dispatchEvent(backspace)

    expect(backspace.defaultPrevented).toBe(false)
    wrapper.unmount()
  })

  it('renders notifications settings from its deep link', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/settings/notifications')
    await router.isReady()
    const wrapper = mount(App, { global: { plugins: [router] } })
    await flushPromises()

    expect(router.currentRoute.value.params.section).toBe('notifications')
    expect(wrapper.find('[data-testid="notification-settings"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // Routing a section is not the same as reaching it: App resolves :section
  // itself, and a section it does not recognize silently renders the default
  // pane. Clicking each nav entry is the only check that covers both halves.
  it.each(applicationSettingsSections)('navigates to the %s settings section from its nav entry', async (section) => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/settings')
    await router.isReady()
    const wrapper = mount(App, { global: { plugins: [router] } })
    await flushPromises()

    await wrapper.get(`[data-testid="settings-category-${section}"]`).trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.params.section).toBe(section)
    expect(
      wrapper.get(`[data-testid="settings-category-${section}"]`).attributes('aria-current'),
      `the ${section} nav entry is not marked current — App resolved :section to another pane`,
    ).toBe('true')
    wrapper.unmount()
  })

  it('renders developer tools from its deep link', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/dev')
    await router.isReady()
    const wrapper = mount(App, { global: { plugins: [router] } })

    try {
      await flushPromises()
      expect(router.currentRoute.value.name).toBe('dev')
      await vi.waitFor(() => {
        expect(wrapper.find('[data-testid="dev-view"]').exists()).toBe(true)
      })
    } finally {
      wrapper.unmount()
    }
  })

  it('routes DetailPane Edit to actions settings', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/')
    await router.isReady()
    const wrapper = mount(App, {
      global: {
        plugins: [router],
        stubs: { DetailPane: { template: '<button data-testid="detail-edit" @click="$emit(\'edit\')" />', emits: ['edit'] } },
      },
    })
    await flushPromises()
    await wrapper.get('[data-testid="detail-edit"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value).toMatchObject({ name: 'application-settings', params: { section: 'actions' } })
    expect(wrapper.find('[data-testid="actions-settings"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders the actions settings deep-link and preserves it through back/forward history', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/settings/actions')
    await router.isReady()
    const wrapper = mount(App, { global: { plugins: [router] } })
    await flushPromises()
    expect(router.currentRoute.value.params.section).toBe('actions')
    expect(wrapper.find('[data-testid="actions-settings"]').exists()).toBe(true)

    await router.push('/settings/integrations')
    await flushPromises()
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.params.section).toBe('actions')
    expect(wrapper.find('[data-testid="actions-settings"]').exists()).toBe(true)
    router.forward()
    await flushPromises()
    expect(router.currentRoute.value.params.section).toBe('integrations')
    wrapper.unmount()
  })

  it('records feed and unread navigation in back/forward history', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.find('[data-testid="sidebar-feed"][data-id="personal/desktop"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.feed).toBe('personal/desktop')
    expect(wrapper.find('[data-testid="sidebar-feed"][data-id="personal/desktop"]').classes()).toContain('sidebar-entry-selected')

    await wrapper.find('[data-testid="filter-unread"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ feed: 'personal/desktop', unread: '1' })

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ feed: 'personal/desktop' })
    expect(wrapper.find('[data-testid="filter-all"]').classes()).toContain('active')

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    // A bare feed route selects the workspace default: the last-selected feed.
    expect(wrapper.find('[data-testid="sidebar-feed"][data-id="personal/desktop"]').classes()).toContain('sidebar-entry-selected')

    router.forward()
    await flushPromises()
    expect(router.currentRoute.value.query.feed).toBe('personal/desktop')

    wrapper.unmount()
  })

  it('routes to trash and loads it via the dedicated trash query', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.get('[data-testid="sidebar-trash"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ view: 'trash' })
    expect(mocks.ListInboxItemsTrash).toHaveBeenLastCalledWith('personal', 500)
    expect(wrapper.get('[data-testid="sidebar-trash"]').classes()).toContain('footer-entry-selected')
    wrapper.unmount()
  })

  it('clears stale observed activity while the selected item timeline loads or fails', async () => {
    const items = [
      { id: 1, profileId: 'personal', sourceKind: 'github', sourceScope: '', externalId: 'pr-1', title: 'First', url: '', payload: { kind: 'PR', repo: 'acme/app', num: 1, author: 'hay' }, revision: 1, unread: true, lifecycle: 'active', firstSeenAt: 1, lastEventAt: 2 },
      { id: 2, profileId: 'personal', sourceKind: 'github', sourceScope: '', externalId: 'pr-2', title: 'Second', url: '', payload: { kind: 'PR', repo: 'acme/app', num: 2, author: 'hay' }, revision: 1, unread: true, lifecycle: 'active', firstSeenAt: 1, lastEventAt: 2 },
    ]
    let rejectSecond!: (error: Error) => void
    const secondEvents = new Promise<never>((_, reject) => { rejectSecond = reject })
    mocks.ListInboxItemsByFeed.mockResolvedValue(items)
    mocks.InboxItemEvents.mockImplementation((id: number) => id === 1
      ? Promise.resolve([{ id: 1, itemId: 1, kind: 'observed', transition: 'none', attention: 'activity', summary: 'first event', detail: {}, createdAt: 1 }])
      : secondEvents)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const wrapper = await mountApp()
    expect(wrapper.get('[data-testid="observed-activity"]').text()).toContain('first event')

    await wrapper.findAll('[data-testid="feed-item"]')[1]!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="observed-activity"]').exists()).toBe(false)

    rejectSecond(new Error('events unavailable'))
    await flushPromises()
    expect(wrapper.find('[data-testid="observed-activity"]').exists()).toBe(false)
    expect(warn).toHaveBeenCalledWith('Unable to load inbox item events', expect.any(Error))
    wrapper.unmount()
  })

  it('guards native back navigation when the flow has un-deployed changes', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await wrapper.find('[data-testid="sidebar-edit-flow"]').trigger('click')
    await flushPromises()

    const session = useFlowsSession()
    session.addNode('feed')
    router.back()
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('flows')
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).not.toBeNull()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-discard"]')?.click()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('feed')
    expect(session.dirty.value).toBe(false)

    wrapper.unmount()
  })

  it('deletes the active profile from profile settings, then falls back to onboarding', async () => {
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="sidebar-delete-profile"]').exists()).toBe(false)
    await wrapper.find('[data-testid="sidebar-open-settings"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="profile-settings-danger"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="profile-settings-delete"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="delete-profile-modal"]')).not.toBeNull()

    mocks.ListFlows.mockResolvedValue([])
    document.querySelector<HTMLButtonElement>('[data-testid="delete-profile-confirm"]')?.click()
    await flushPromises()

    expect(mocks.DeleteFlow).toHaveBeenCalledWith('personal')
    expect(document.querySelector('[data-testid="delete-profile-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="onboarding"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('binds the active profile draft even with the flows canvas closed (hc-8ft4yhm6)', async () => {
    const wrapper = await mountApp()

    // GetLayout/NodeRuns are only ever called from usePipelineEditor's
    // selectFlow — never from useFeedState — so seeing them here proves
    // the app-wide session selected and loaded the active profile draft even
    // though the flows canvas was never opened.
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)
    expect(mocks.GetLayout).toHaveBeenCalledWith('personal')
    expect(mocks.NodeRuns).toHaveBeenCalledWith('personal', 100)

    wrapper.unmount()
  })

  it('reconciles deployed runtimes when flows:updated arrives with the canvas closed', async () => {
    const wrapper = await mountApp()
    const getFlowCalls = mocks.GetFlow.mock.calls.length
    const handler = mocks.On.mock.calls.find(([event]) => event === 'flows:updated')?.[1] as (() => void) | undefined

    expect(handler).toBeDefined()
    handler?.()
    await vi.waitFor(() => expect(mocks.GetFlow.mock.calls.length).toBeGreaterThan(getFlowCalls))

    wrapper.unmount()
  })

  it('the titlebar error chip deep-links to the first failing node, even with the canvas closed', async () => {
    mocks.NodeRuns.mockResolvedValue([
      { flowId: 'personal', nodeId: 'src', ok: false, inCount: 0, outCount: 0, dropCount: 0, err: 'boom', durMs: 1, endedAt: 0 },
    ])

    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)
    const chip = wrapper.find('[data-testid="titlebar-error-chip"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toContain('1 error')

    await chip.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(true)
    const session = useFlowsSession()
    expect(session.flowFocusNodeId.value).toBe('src')

    wrapper.unmount()
  })

  it('registers a ⌘K "jump to node" command per node in the active flow', async () => {
    const wrapper = await mountApp()
    const { results, query } = useCommandPalette()
    query.value = ''

    const ids = results.value.map((cmd) => cmd.id)
    expect(ids).toContain('flow:node:src')
    expect(ids).toContain('flow:node:desktop')

    const nodeCmd = results.value.find((cmd) => cmd.id === 'flow:node:desktop')
    expect(nodeCmd?.title).toBe('Jump to node: Desktop UI')

    nodeCmd?.run()
    await flushPromises()
    expect(useFlowsSession().flowFocusNodeId.value).toBe('desktop')

    wrapper.unmount()
  })

  // ── Un-deployed changes guard (hc-sx4k3c7k) ──────────────────────────────

  it('shows the un-deployed changes badge in the sidebar once the flow is dirty', async () => {
    const wrapper = await mountApp()
    expect(wrapper.find('[data-testid="undeployed-badge"]').exists()).toBe(false)

    useFlowsSession().addNode('feed')
    await flushPromises()

    expect(wrapper.find('[data-testid="undeployed-badge"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('exiting the canvas via the profile rail while dirty prompts a confirm instead of leaving immediately; Cancel stays in the canvas', async () => {
    const wrapper = await mountApp()
    await wrapper.find('[data-testid="sidebar-edit-flow"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(true)

    const session = useFlowsSession()
    session.addNode('feed')
    expect(session.dirty.value).toBe(true)

    await wrapper.find('[data-testid="profile-tile"][data-id="personal"]').trigger('click')
    await flushPromises()

    // Still in the canvas — the exit was deferred behind the confirm modal.
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(true)
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).not.toBeNull()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-cancel"]')?.click()
    await flushPromises()

    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(true) // cancel aborted the exit
    expect(session.dirty.value).toBe(true) // draft untouched

    wrapper.unmount()
  })

  it('exiting the canvas via the profile rail while dirty: Deploy saves the draft then returns to the feed view', async () => {
    const wrapper = await mountApp()
    await wrapper.find('[data-testid="sidebar-edit-flow"]').trigger('click')
    await flushPromises()

    const session = useFlowsSession()
    session.addNode('feed')

    await wrapper.find('[data-testid="profile-tile"][data-id="personal"]').trigger('click')
    await flushPromises()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-deploy"]')?.click()
    await flushPromises()

    expect(mocks.SaveFlow).toHaveBeenCalled()
    expect(mocks.SaveLayout).toHaveBeenCalled()
    expect(session.dirty.value).toBe(false)
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('exiting the canvas via the profile rail while dirty: Discard drops the draft (reloads from disk) then returns to the feed view', async () => {
    const wrapper = await mountApp()
    await wrapper.find('[data-testid="sidebar-edit-flow"]').trigger('click')
    await flushPromises()

    const session = useFlowsSession()
    session.addNode('feed')
    const getFlowCallsBefore = mocks.GetFlow.mock.calls.length

    await wrapper.find('[data-testid="profile-tile"][data-id="personal"]').trigger('click')
    await flushPromises()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-discard"]')?.click()
    await flushPromises()

    expect(mocks.SaveFlow).not.toHaveBeenCalled()
    expect(mocks.GetFlow.mock.calls.length).toBeGreaterThan(getFlowCallsBefore) // discard reloaded from disk
    expect(session.dirty.value).toBe(false)
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="flows-view"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('switching profiles from the rail while dirty prompts a confirm instead of switching immediately; Cancel stays on the current profile', async () => {
    mocks.ListFlows.mockResolvedValue([
      { id: 'personal', name: 'Personal', enabled: true, valid: true },
      { id: 'work', name: 'Work', enabled: true, valid: true },
    ])
    mocks.GetFlow.mockImplementation(async (id: string) =>
      id === 'work' ? { id: 'work', name: 'Work', enabled: true, nodes: [], wires: [] } : flow,
    )

    const wrapper = await mountApp()
    const session = useFlowsSession()
    session.addNode('feed') // dirties the active ("personal") flow's draft
    expect(session.dirty.value).toBe(true)

    await wrapper.find('[data-testid="profile-tile"][data-id="work"]').trigger('click')
    await flushPromises()

    // Still on the personal profile — the switch was deferred behind the confirm modal.
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Personal')
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).not.toBeNull()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-cancel"]')?.click()
    await flushPromises()

    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Personal') // cancel aborted the switch
    expect(session.dirty.value).toBe(true) // draft untouched
    expect(mocks.GetLayout).not.toHaveBeenCalledWith('work')

    wrapper.unmount()
  })

  it('switching profiles from the rail while dirty: Deploy saves the draft then switches profiles', async () => {
    mocks.ListFlows.mockResolvedValue([
      { id: 'personal', name: 'Personal', enabled: true, valid: true },
      { id: 'work', name: 'Work', enabled: true, valid: true },
    ])
    mocks.GetFlow.mockImplementation(async (id: string) =>
      id === 'work' ? { id: 'work', name: 'Work', enabled: true, nodes: [], wires: [] } : flow,
    )

    const wrapper = await mountApp()
    const session = useFlowsSession()
    session.addNode('feed')

    await wrapper.find('[data-testid="profile-tile"][data-id="work"]').trigger('click')
    await flushPromises()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-deploy"]')?.click()
    await flushPromises()

    expect(mocks.SaveFlow).toHaveBeenCalled()
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Work')
    expect(mocks.GetLayout).toHaveBeenCalledWith('work')

    wrapper.unmount()
  })

  it('switching profiles from the rail while dirty: Discard drops the draft then switches profiles', async () => {
    mocks.ListFlows.mockResolvedValue([
      { id: 'personal', name: 'Personal', enabled: true, valid: true },
      { id: 'work', name: 'Work', enabled: true, valid: true },
    ])
    mocks.GetFlow.mockImplementation(async (id: string) =>
      id === 'work' ? { id: 'work', name: 'Work', enabled: true, nodes: [], wires: [] } : flow,
    )

    const wrapper = await mountApp()
    const session = useFlowsSession()
    session.addNode('feed')

    await wrapper.find('[data-testid="profile-tile"][data-id="work"]').trigger('click')
    await flushPromises()

    document.querySelector<HTMLButtonElement>('[data-testid="unsaved-flow-discard"]')?.click()
    await flushPromises()

    expect(mocks.SaveFlow).not.toHaveBeenCalled()
    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Work')
    expect(mocks.GetLayout).toHaveBeenCalledWith('work')

    wrapper.unmount()
  })

  it('switching profiles from the rail is instant (no confirm) when the flow is not dirty', async () => {
    mocks.ListFlows.mockResolvedValue([
      { id: 'personal', name: 'Personal', enabled: true, valid: true },
      { id: 'work', name: 'Work', enabled: true, valid: true },
    ])
    mocks.GetFlow.mockImplementation(async (id: string) =>
      id === 'work' ? { id: 'work', name: 'Work', enabled: true, nodes: [], wires: [] } : flow,
    )

    const wrapper = await mountApp()
    expect(useFlowsSession().dirty.value).toBe(false)

    await wrapper.find('[data-testid="profile-tile"][data-id="work"]').trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-testid="unsaved-flow-changes-modal"]')).toBeNull()
    expect(wrapper.find('[data-testid="sidebar-profile-name"]').text()).toBe('Work')

    wrapper.unmount()
  })

  it('refreshes the feed on "inbox:updated" — the engine commits before it announces', async () => {
    const wrapper = await mountApp()

    mocks.FeedCounts.mockClear()
    const inboxHandler = mocks.On.mock.calls.find(([event]) => event === 'inbox:updated')?.[1] as (() => void) | undefined
    expect(inboxHandler).toBeDefined()

    inboxHandler?.()
    await vi.waitFor(() => expect(mocks.FeedCounts).toHaveBeenCalled())

    wrapper.unmount()
  })

  it('does not re-read on "log:appended" — a log row may route nowhere, and the engine has not committed yet', async () => {
    const wrapper = await mountApp()

    const logHandler = mocks.On.mock.calls.find(([event]) => event === 'log:appended')?.[1]
    expect(logHandler).toBeUndefined()

    wrapper.unmount()
  })

})
