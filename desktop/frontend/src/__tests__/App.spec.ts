import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { useCommandPalette } from '../composables/useCommands'
import { resetFlowsSessionForTests, useFlowsSession } from '../pipeline/composables/useFlowsSession'
import { resetNotificationSettingsForTests } from '../composables/useNotificationSettings'
import { resetPopupTerminalForTests, usePopupTerminal } from '../composables/usePopupTerminal'
import { resetLaunchersForTests } from '../composables/useLaunchers'
import { useKeybindings } from '../composables/useKeybindings'
import { resetTerminalAvailabilityForTests } from '../composables/useTerminalAvailability'
import { resetTerminalSessionsForTests } from '../composables/useTerminalSessions'
import { resetAgentWorkspacesForTests } from '../composables/useAgentWorkspaces'
import { applicationSettingsSections, createAppRouter } from '../router'
import { setTerminalTreeHandles, type TerminalTreeHandles } from '../lib/terminalTree'

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
  // terminalservice
  TerminalAvailable: vi.fn(),
  TerminalEndpoint: vi.fn(),
  TerminalModeEnabled: vi.fn(),
  // popupterminalservice
  PopupAvailable: vi.fn(),
  PopupEndpoint: vi.fn(),
  PopupLaunchers: vi.fn(),
  // agentsservice
  AgentsAvailable: vi.fn(),
  AgentsEndpoint: vi.fn(),
  AgentsModeEnabled: vi.fn(),
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
  ListSessions: vi.fn().mockResolvedValue([]),
  SessionStatuses: vi.fn().mockResolvedValue({ items: [], pollIntervalMs: 60_000 }),
  TerminalActionViews: vi.fn().mockResolvedValue([]),
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
  AppearanceSettings: vi.fn().mockResolvedValue({ theme: '', terminalFontSize: '', terminalFontFamily: '', terminalFontWeight: 0, terminalFontWeightBold: 0, terminalShowWindows: true, terminalPoolSize: 3 }),
  MonospaceFonts: vi.fn().mockResolvedValue([]),
  SetTheme: vi.fn(),
  SetTerminalFontSize: vi.fn(),
  SetTerminalFontFamily: vi.fn(),
  SetTerminalFontWeights: vi.fn(),
  SetTerminalShowWindows: vi.fn(),
  SetTerminalPoolSize: vi.fn(),
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

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Available: mocks.TerminalAvailable,
  Endpoint: mocks.TerminalEndpoint,
  Enabled: mocks.TerminalModeEnabled,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/popupterminalservice', () => ({
  Available: mocks.PopupAvailable,
  Endpoint: mocks.PopupEndpoint,
  Launchers: mocks.PopupLaunchers,
}))

vi.mock('../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.AgentsAvailable,
  Endpoint: mocks.AgentsEndpoint,
  Enabled: mocks.AgentsModeEnabled,
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

function inboxItems() {
  return [1, 2].map((n) => ({
    id: n, profileId: 'personal', sourceKind: 'github', sourceScope: '', externalId: `pr-${n}`,
    title: n === 1 ? 'First' : 'Second', url: '', payload: { kind: 'PR', repo: 'acme/app', num: n, author: 'hay' },
    revision: 1, unread: false, lifecycle: 'active', firstSeenAt: 1, lastEventAt: 2,
  }))
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

// Terminal mode is mounted once and hidden on a trip to the hub, so whether it
// is the surface on screen is a question about visibility, never about the
// element being there. Read off v-show's own inline display rather than through
// isVisible(): that goes to getComputedStyle, which happy-dom does not resolve
// for a tree VTU never attached to the document.
function terminalOnScreen(wrapper: VueWrapper): boolean {
  const mode = wrapper.find('[data-testid="terminal-mode"]')
  return mode.exists() && !(mode.attributes('style') ?? '').includes('display: none')
}

// Same shape as terminalOnScreen: the Agents area is mount-once/v-show too.
function agentsOnScreen(wrapper: VueWrapper): boolean {
  const mode = wrapper.find('[data-testid="agents-mode"]')
  return mode.exists() && !(mode.attributes('style') ?? '').includes('display: none')
}

// Stands in for TerminalMode's registration. Every handle is a spy so a test
// only has to name the one it asserts on, and a command that reached the wrong
// handle still shows up.
function stubTerminalTree(overrides: Partial<TerminalTreeHandles> = {}): TerminalTreeHandles {
  const handles: TerminalTreeHandles = {
    focusTree: vi.fn(), focusPane: vi.fn(), focusFilter: vi.fn(),
    selectWindow: vi.fn(), newWindow: vi.fn(), closeWindow: vi.fn(), stepWindow: vi.fn(),
    ...overrides,
  }
  setTerminalTreeHandles(handles)
  return handles
}

// A pane in the document, so a keydown dispatched on it reads as one a focused
// terminal would have taken.
function focusedPane(): HTMLElement {
  const pane = document.createElement('div')
  pane.setAttribute('data-terminal-input-scope', '')
  document.body.append(pane)
  return pane
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
    // The pop-up panel state and the launcher commands are module singletons
    // too, and a launcher registered by one test would stay bindable in the next.
    resetPopupTerminalForTests()
    resetLaunchersForTests()
    useKeybindings().clearAll()
    resetTerminalAvailabilityForTests()
    resetTerminalSessionsForTests()
    resetAgentWorkspacesForTests()
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
    mocks.TerminalModeEnabled.mockResolvedValue(true)
    mocks.PopupAvailable.mockResolvedValue({ available: true, reason: '' })
    mocks.PopupEndpoint.mockResolvedValue({ httpBaseURL: '', wsURL: '', token: '' })
    mocks.PopupLaunchers.mockResolvedValue([])
    mocks.TerminalAvailable.mockResolvedValue({ available: false, reason: 'tmux is not installed.' })
    mocks.TerminalEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
    // Agents mode defaults off in these tests, same as terminal mode defaults
    // on: most tests are not about the mode switch, and a disabled area keeps
    // the title bar's default assertions (Inbox|Code, no Agents segment) true
    // without every test having to say so.
    mocks.AgentsModeEnabled.mockResolvedValue(false)
    mocks.AgentsAvailable.mockResolvedValue({ available: false, reason: 'The Agents area is off.' })
    mocks.AgentsEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
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

  // A launcher is a line of actions.yml that has to become both a palette row
  // and a chord of its own — this is where those two meet the app.
  it('offers a configured launcher in the palette and opens it on the session it is attached to', async () => {
    mocks.PopupLaunchers.mockResolvedValue([{ id: 'lazygit', label: 'lazygit', icon: 'git-branch', requiresSession: true }])
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { results, query } = useCommandPalette()
    query.value = ''
    expect(results.value.map((cmd) => cmd.id)).toContain('launcher.lazygit')

    useKeybindings().addBinding('launcher.lazygit', 'alt+g')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'g', altKey: true }))

    const popup = usePopupTerminal()
    expect(popup.visible.value).toBe(true)
    expect(popup.request.value).toEqual({ launcher: 'lazygit', sessionSlug: 'hive-fix-parser' })

    // The chord that opened it puts it away, so quitting the program is not the
    // only way out.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'g', altKey: true }))
    expect(popup.visible.value).toBe(false)

    wrapper.unmount()
  })

  // The bug this is here for: `lazygit` opened from the feed used to start in
  // the home directory and present a failed TUI. A launcher that runs in a
  // session's checkout is not offered where there is no session, and its chord
  // is not dispatched there either (ADR quick-terminal-launchers-are-session-scoped).
  it('withholds a session-scoped launcher outside a session, from the palette and from its chord', async () => {
    mocks.PopupLaunchers.mockResolvedValue([{ id: 'lazygit', label: 'lazygit', icon: 'git-branch', requiresSession: true }])
    const { wrapper, router } = await mountAppWithRouter()
    useKeybindings().addBinding('launcher.lazygit', 'alt+g')

    const { results, query } = useCommandPalette()
    const popup = usePopupTerminal()
    query.value = ''

    // On the feed: no session, so nothing to run in.
    expect(results.value.map((cmd) => cmd.id)).not.toContain('launcher.lazygit')
    const onFeed = new KeyboardEvent('keydown', { key: 'g', altKey: true, cancelable: true })
    window.dispatchEvent(onFeed)
    expect(popup.visible.value).toBe(false)
    expect(onFeed.defaultPrevented).toBe(false)

    // Terminal mode with nothing attached is the session picker, and a launcher
    // has no more to work with there than it does on the feed.
    await router.push('/terminal')
    await flushPromises()
    expect(results.value.map((cmd) => cmd.id)).not.toContain('launcher.lazygit')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'g', altKey: true }))
    expect(popup.visible.value).toBe(false)

    wrapper.unmount()
  })

  // A launcher pinned to a directory carries the context it needs in the
  // catalog, so it stays reachable from anywhere — the scope rule is about the
  // ones that resolve their directory from the session.
  it('keeps a launcher with its own working directory reachable off a session', async () => {
    mocks.PopupLaunchers.mockResolvedValue([{ id: 'dotfiles', label: 'Edit dotfiles', icon: 'folder', requiresSession: false }])
    const wrapper = await mountApp()

    const { results, query } = useCommandPalette()
    query.value = ''
    expect(results.value.map((cmd) => cmd.id)).toContain('launcher.dotfiles')

    useKeybindings().addBinding('launcher.dotfiles', 'alt+d')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'd', altKey: true }))

    const popup = usePopupTerminal()
    expect(popup.visible.value).toBe(true)
    expect(popup.request.value).toEqual({ launcher: 'dotfiles', sessionSlug: undefined })

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
    expect(localStorage.getItem('hive.panel.sidebar.collapsed')).toBe('true')

    await wrapper.find('[data-testid="titlebar-toggle-sidebar"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)
    expect(localStorage.getItem('hive.panel.sidebar.collapsed')).toBe('false')

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

  it('reopens the collapsed preview on a double-click, not on the click that selects', async () => {
    mocks.ListInboxItemsByFeed.mockResolvedValue(inboxItems())
    const wrapper = await mountApp()

    await wrapper.get('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    // The first click of the gesture selects, and must leave the pane shut.
    await wrapper.findAll('[data-testid="feed-item"]')[1]!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="feed-item"]')[1]!.classes()).toContain('selected')
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    await wrapper.findAll('[data-testid="feed-item"]')[1]!.trigger('dblclick')
    await flushPromises()
    expect(wrapper.get('[data-testid="detail-pane"] h1').text()).toBe('Second')

    // Double-clicking the row that is already selected reopens too — the
    // gesture is the request to read it, not a selection change.
    await wrapper.get('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    await wrapper.findAll('[data-testid="feed-item"]')[1]!.trigger('dblclick')
    await flushPromises()
    expect(wrapper.get('[data-testid="detail-pane"] h1').text()).toBe('Second')

    wrapper.unmount()
  })

  it('navigates the feed by keyboard silently, and reopens the preview on the row it activates', async () => {
    mocks.ListInboxItemsByFeed.mockResolvedValue(inboxItems())
    const wrapper = await mountApp()

    await wrapper.get('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'j' }))
    await flushPromises()
    expect(wrapper.findAll('[data-testid="feed-item"]')[1]!.classes()).toContain('selected')
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    await wrapper.findAll('[data-testid="feed-item"]')[1]!.trigger('keydown.enter')
    await flushPromises()
    expect(wrapper.get('[data-testid="detail-pane"] h1').text()).toBe('Second')

    wrapper.unmount()
  })

  it('leaves the collapsed preview shut for row controls and the list header menus', async () => {
    mocks.ListInboxItemsByFeed.mockResolvedValue(inboxItems())
    mocks.MarkInboxItemUnread.mockImplementation(async (id: number, revision: number, unread: boolean) =>
      ({ ...inboxItems().find((item) => item.id === id)!, revision: revision + 1, unread }))
    const wrapper = await mountApp()

    await wrapper.get('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()

    const row = () => wrapper.findAll('[data-testid="feed-item"]')[1]!
    await row().get('[data-testid="row-archive"]').trigger('click')
    await flushPromises()
    expect(mocks.ToggleInboxItemArchived).toHaveBeenCalledWith(2, 1)
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    await row().get('[data-testid="row-menu-toggle"]').trigger('click')
    await flushPromises()
    await row().get('[data-testid="menu-toggle-read"]').trigger('click')
    await flushPromises()
    expect(mocks.MarkInboxItemUnread).toHaveBeenCalledWith(2, 1, true)
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    await wrapper.get('[data-testid="view-menu-toggle"]').trigger('click')
    await wrapper.get('[data-testid="view-sort-oldest"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    // A double-click landing inside the hover pill is aimed at its buttons, so
    // it must not reach the row underneath either.
    await row().get('[data-testid="row-hover-actions"]').trigger('dblclick')
    await flushPromises()
    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('persists the reopen as the remembered last state', async () => {
    mocks.ListInboxItemsByFeed.mockResolvedValue(inboxItems())
    const wrapper = await mountApp()

    await wrapper.get('[data-testid="titlebar-toggle-preview"]').trigger('click')
    await flushPromises()
    expect(localStorage.getItem('hive.panel.detailpane.collapsed')).toBe('true')

    await wrapper.findAll('[data-testid="feed-item"]')[0]!.trigger('dblclick')
    await flushPromises()
    expect(localStorage.getItem('hive.panel.detailpane.collapsed')).toBe('false')

    wrapper.unmount()
  })

  it('starts collapsed when that is the persisted last state', async () => {
    localStorage.setItem('hive.panel.detailpane.collapsed', 'true')
    mocks.ListInboxItemsByFeed.mockResolvedValue(inboxItems())
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="detail-pane"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('opens profile settings from the sidebar gear and application settings from the rail', async () => {
    const wrapper = await mountApp()

    await wrapper.find('[data-testid="sidebar-open-settings"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="profile-settings-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="settings-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="profile-settings-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(true)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('uses route history for settings pages and categories', async () => {
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(wrapper.find('[data-testid="settings-general"]').exists()).toBe(true)

    await wrapper.find('[data-testid="settings-category-integrations"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.params.section).toBe('integrations')
    expect(wrapper.find('[data-testid="settings-integrations"]').exists()).toBe(true)

    router.back()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(router.currentRoute.value.params.section).toBe('')
    expect(wrapper.find('[data-testid="settings-general"]').exists()).toBe(true)

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

  it('never renders the Inbox|Code toggle while experimental.terminal is off', async () => {
    mocks.TerminalModeEnabled.mockResolvedValue(false)
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="titlebar-mode-terminal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="titlebar-mode-hub"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('swaps the whole hub for terminal mode and back from the title-bar toggle', async () => {
    mocks.TerminalAvailable.mockResolvedValue({ available: false, reason: 'tmux is not installed.' })
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    // Terminal mode is async-imported, so it lands a tick after the toggle.
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))
    await flushPromises()

    // The mode is a route, so the toggle is ordinary navigation.
    expect(router.currentRoute.value.name).toBe('terminal')
    // The toggle is never gated on availability; the reason shows up inside.
    expect(wrapper.get('[data-testid="terminal-unavailable-reason"]').text()).toBe('tmux is not installed.')
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(false)
    // Terminal owns a left panel too, so its toggle stays live; the feed-only
    // preview remains unavailable.
    expect(wrapper.get('[data-testid="titlebar-toggle-sidebar"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="titlebar-toggle-preview"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="titlebar-mode-hub"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('feed')
    expect(terminalOnScreen(wrapper)).toBe(false)
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(true)
    // Hidden, not unmounted: the pool behind it holds live tmux clients and
    // xterm screens, and re-entry must not pay to build them again.
    expect(wrapper.find('[data-testid="terminal-mode"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('restores and toggles the terminal sidebar independently of the feed sidebar', async () => {
    localStorage.setItem('hive.panel.sidebar.collapsed', 'false')
    localStorage.setItem('hive.panel.terminal.sidebar.collapsed', 'true')
    mocks.TerminalAvailable.mockResolvedValue({ available: true, reason: '' })
    const { wrapper } = await mountAppWithRouter()

    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))
    await flushPromises()

    const toggle = wrapper.get('[data-testid="titlebar-toggle-sidebar"]')
    expect(toggle.attributes('disabled')).toBeUndefined()
    expect(toggle.attributes('aria-label')).toBe('Show sidebar')
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(false)

    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
    expect(localStorage.getItem('hive.panel.terminal.sidebar.collapsed')).toBe('false')

    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(false)
    expect(localStorage.getItem('hive.panel.terminal.sidebar.collapsed')).toBe('true')

    await wrapper.get('[data-testid="titlebar-mode-hub"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-profile-header"]').exists()).toBe(true)
    expect(localStorage.getItem('hive.panel.sidebar.collapsed')).toBe('false')

    wrapper.unmount()
  })

  it('keeps the title-bar navigation live inside terminal mode', async () => {
    mocks.TerminalAvailable.mockResolvedValue({ available: false, reason: 'tmux is not installed.' })
    const { wrapper, router } = await mountAppWithRouter()

    // Start somewhere other than the feed so "back to the hub" is observable
    // as "back to where the hub was", not "back to the default feed".
    await router.push({ name: 'application-settings', params: { section: 'integrations' } })
    await flushPromises()

    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))

    // The Inbox toggle lands on the page that mode was left on, not the feed.
    await wrapper.get('[data-testid="titlebar-mode-hub"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('application-settings')
    expect(router.currentRoute.value.params.section).toBe('integrations')

    // Activity is reachable from inside terminal mode without toggling first.
    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))
    await wrapper.get('[data-testid="titlebar-activity"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('activity')
    expect(terminalOnScreen(wrapper)).toBe(false)

    // The mode is history like any page: Back returns to the terminal route.
    router.back()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('terminal'))
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))

    wrapper.unmount()
  })

  it('re-enters terminal mode on the session it was left attached to', async () => {
    mocks.TerminalAvailable.mockResolvedValue({ available: true, reason: '' })
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    await vi.waitFor(() => expect(terminalOnScreen(wrapper)).toBe(true))
    await router.replace({ name: 'terminal', params: { slug: 'api-fix' }, query: { window: '@3' } })
    await flushPromises()

    await wrapper.get('[data-testid="titlebar-mode-hub"]').trigger('click')
    await flushPromises()
    expect(terminalOnScreen(wrapper)).toBe(false)

    // Straight back to the attached session and window, not through the bare
    // picker route — that pass detached the pool entry and blanked the pane.
    await wrapper.get('[data-testid="titlebar-mode-terminal"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('terminal')
    expect(router.currentRoute.value.params.slug).toBe('api-fix')
    expect(router.currentRoute.value.query.window).toBe('@3')

    wrapper.unmount()
  })

  it('never renders the Agents segment while experimental.agents is off', async () => {
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="titlebar-mode-agents"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('swaps the whole hub for the Agents area and back from the title-bar toggle, hiding rather than unmounting it', async () => {
    mocks.AgentsModeEnabled.mockResolvedValue(true)
    mocks.AgentsAvailable.mockResolvedValue({ available: false, reason: 'no ptyterm on this build.' })
    const { wrapper, router } = await mountAppWithRouter()

    await wrapper.get('[data-testid="titlebar-mode-agents"]').trigger('click')
    // AgentsMode is async-imported, so it lands a tick after the toggle.
    await vi.waitFor(() => expect(agentsOnScreen(wrapper)).toBe(true))
    await flushPromises()

    // The mode is a route, so the toggle is ordinary navigation.
    expect(router.currentRoute.value.name).toBe('agents')
    // The toggle is never gated on availability; the reason shows up inside.
    expect(wrapper.get('[data-testid="agents-unavailable-reason"]').text()).toBe('no ptyterm on this build.')
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(false)

    await wrapper.get('[data-testid="titlebar-mode-hub"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('feed')
    expect(agentsOnScreen(wrapper)).toBe(false)
    expect(wrapper.find('[data-testid="profile-tile"]').exists()).toBe(true)
    // Hidden, not unmounted: leaving the area must not end a live session's
    // pane (ADR terminal-mode-is-hidden-not-unmounted).
    expect(wrapper.find('[data-testid="agents-mode"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('shows Inbox | Agents when only experimental.agents is on — the group renders on the second mode, not on terminal specifically', async () => {
    mocks.TerminalModeEnabled.mockResolvedValue(false)
    mocks.AgentsModeEnabled.mockResolvedValue(true)
    const wrapper = await mountApp()

    expect(wrapper.find('[data-testid="titlebar-mode-hub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="titlebar-mode-terminal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="titlebar-mode-agents"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('lets a focused terminal keep every key a pane can use', async () => {
    const wrapper = await mountApp()
    const { open: paletteOpen } = useCommandPalette()

    const pane = document.createElement('div')
    pane.setAttribute('data-terminal-input-scope', '')
    document.body.append(pane)

    // Ctrl+K is readline's kill-to-end-of-line, and `mod+k` cannot tell it from
    // ⌘K — so the pane keeps it even though it resolves to the palette.
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true }))
    // A bare navigation key is the pane's outright.
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }))
    await flushPromises()
    expect(paletteOpen.value).toBe(false)

    pane.remove()
    wrapper.unmount()
  })

  // The palette is how you get back out of a pane, so it is the exception to
  // the rule above — on the modifiers a terminal never wants.
  it('opens the palette over a focused terminal on Cmd, and on Ctrl+Shift', async () => {
    const wrapper = await mountApp()
    const { open: paletteOpen } = useCommandPalette()

    const pane = document.createElement('div')
    pane.setAttribute('data-terminal-input-scope', '')
    document.body.append(pane)

    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    await flushPromises()
    expect(paletteOpen.value).toBe(true)

    paletteOpen.value = false
    await flushPromises()
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, shiftKey: true, bubbles: true }))
    await flushPromises()
    expect(paletteOpen.value).toBe(true)

    paletteOpen.value = false
    pane.remove()
    wrapper.unmount()
  })

  // The other way out of a pane. Only this half of the focus pair pierces: the
  // chord that moves focus *into* a pane is unreachable from inside one.
  it('reaches the session tree from inside a focused terminal', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { focusTree } = stubTerminalTree()
    const pane = focusedPane()

    const event = new KeyboardEvent('keydown', { key: 'ArrowLeft', metaKey: true, bubbles: true, cancelable: true })
    pane.dispatchEvent(event)
    await flushPromises()

    expect(focusTree).toHaveBeenCalled()
    // Swallowed here so the webview cannot also read ⌘← as browser Back.
    expect(event.defaultPrevented).toBe(true)

    setTerminalTreeHandles(null)
    pane.remove()
    wrapper.unmount()
  })

  // Same reason: the window you are jumping away from is holding the keyboard.
  it('jumps to a numbered window from inside a focused terminal', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { selectWindow } = stubTerminalTree()
    const pane = focusedPane()

    const event = new KeyboardEvent('keydown', { key: '3', metaKey: true, bubbles: true, cancelable: true })
    pane.dispatchEvent(event)
    await flushPromises()

    expect(selectWindow).toHaveBeenCalledWith(3)
    expect(event.defaultPrevented).toBe(true)

    setTerminalTreeHandles(null)
    pane.remove()
    wrapper.unmount()
  })

  // A bare key, so it belongs to whatever holds focus: the tree and the rows
  // answer it, and a focused pane keeps `/` as the character it is.
  it('focuses the session filter on / from the tree, and never from inside a pane', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { focusFilter } = stubTerminalTree()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/' }))
    await flushPromises()
    expect(focusFilter).toHaveBeenCalled()

    vi.mocked(focusFilter).mockClear()
    const pane = focusedPane()
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await flushPromises()
    expect(focusFilter).not.toHaveBeenCalled()

    setTerminalTreeHandles(null)
    pane.remove()
    wrapper.unmount()
  })

  it('leaves the terminal chords alone outside terminal mode', async () => {
    const wrapper = await mountApp()
    const { focusTree, selectWindow, newWindow } = stubTerminalTree()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', metaKey: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '2', metaKey: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 't', metaKey: true }))
    await flushPromises()

    expect(focusTree).not.toHaveBeenCalled()
    expect(selectWindow).not.toHaveBeenCalled()
    expect(newWindow).not.toHaveBeenCalled()

    setTerminalTreeHandles(null)
    wrapper.unmount()
  })

  // The tab chords are dispatched from wherever focus is: the tree answers them
  // as an ordinary terminal-context command.
  it('runs the window lifecycle chords from the session tree', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { newWindow, closeWindow, stepWindow } = stubTerminalTree()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 't', metaKey: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'w', metaKey: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '}', metaKey: true, shiftKey: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '{', metaKey: true, shiftKey: true }))
    await flushPromises()

    expect(newWindow).toHaveBeenCalledTimes(1)
    expect(closeWindow).toHaveBeenCalledTimes(1)
    expect(stepWindow).toHaveBeenNthCalledWith(1, 1)
    expect(stepWindow).toHaveBeenNthCalledWith(2, -1)

    setTerminalTreeHandles(null)
    wrapper.unmount()
  })

  // The pane is where you are when you want another tab, so the chords fire
  // over one — on Command, and on Ctrl+Shift where there is no Command.
  it('runs the window lifecycle chords over a focused terminal, on either platform spelling', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { newWindow, closeWindow, stepWindow } = stubTerminalTree()
    const pane = focusedPane()

    const event = new KeyboardEvent('keydown', { key: 't', metaKey: true, bubbles: true, cancelable: true })
    pane.dispatchEvent(event)
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'W', ctrlKey: true, shiftKey: true, bubbles: true }))
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: '}', ctrlKey: true, shiftKey: true, bubbles: true }))
    await flushPromises()

    expect(newWindow).toHaveBeenCalledTimes(1)
    expect(closeWindow).toHaveBeenCalledTimes(1)
    expect(stepWindow).toHaveBeenCalledWith(1)
    expect(event.defaultPrevented).toBe(true)

    setTerminalTreeHandles(null)
    pane.remove()
    wrapper.unmount()
  })

  // The reason the tab chords escape rather than pierce: where `mod` is Ctrl,
  // Ctrl+T is readline's transpose-chars and Ctrl+W its unix-word-rubout.
  it('leaves a focused terminal the bare Ctrl form of the window chords', async () => {
    const { wrapper, router } = await mountAppWithRouter()
    await router.push('/terminal/hive-fix-parser')
    await flushPromises()

    const { newWindow, closeWindow } = stubTerminalTree()
    const pane = focusedPane()

    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 't', ctrlKey: true, bubbles: true }))
    pane.dispatchEvent(new KeyboardEvent('keydown', { key: 'w', ctrlKey: true, bubbles: true }))
    await flushPromises()

    expect(newWindow).not.toHaveBeenCalled()
    expect(closeWindow).not.toHaveBeenCalled()

    setTerminalTreeHandles(null)
    pane.remove()
    wrapper.unmount()
  })

})
