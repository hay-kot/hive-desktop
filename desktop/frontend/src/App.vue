<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { Events, Window } from '@wailsio/runtime'
import { useStorage } from '@vueuse/core'
import { useRoute, useRouter } from 'vue-router'
import IconLayoutGrid from '~icons/lucide/layout-grid'
import IconList from '~icons/lucide/list'
import IconPalette from '~icons/lucide/palette'
import IconRss from '~icons/lucide/rss'
import IconShare2 from '~icons/lucide/share-2'
import IconWorkflow from '~icons/lucide/workflow'
import TitleBar from './components/TitleBar.vue'
import ProfileRail from './components/ProfileRail.vue'
import SideBar from './components/SideBar.vue'
import FeedList from './components/FeedList.vue'
import DetailPane from './components/DetailPane.vue'
import ActionInputsDialog from './components/ActionInputsDialog.vue'
import CreateSessionDialog from './components/CreateSessionDialog.vue'
import NewSessionDialog from './components/NewSessionDialog.vue'
import ConfirmationDialog from './components/ConfirmationDialog.vue'
import CommandPalette from './components/CommandPalette.vue'
import ReportProblemDialog from './components/ReportProblemDialog.vue'
import ProfileSettingsView from './components/ProfileSettingsView.vue'
import SettingsView from './components/SettingsView.vue'
import FlowsView from './pipeline/components/FlowsView.vue'
import ActivityView from './components/ActivityView.vue'
import DeleteProfileModal from './components/DeleteProfileModal.vue'
import NewProfileModal from './components/NewProfileModal.vue'
import UnsavedFlowChangesModal from './components/UnsavedFlowChangesModal.vue'
import OnboardingScreen from './components/OnboardingScreen.vue'
import ToastStack from './components/ToastStack.vue'
import { useGitHubConnection } from './composables/useGitHubConnection'
import { useNotificationSettings } from './composables/useNotificationSettings'
import { useActivity } from './composables/useActivity'
import { useJobs } from './composables/useJobs'
import { useFeedState } from './composables/useFeedState'
import { useCommands, useCommandPalette, type Command } from './composables/useCommands'
import { useReportDialog } from './composables/useReportDialog'
import { useNewSession } from './composables/useNewSession'
import { usePopupTerminal } from './composables/usePopupTerminal'
import { sessionRepository } from './composables/useTerminalSessions'
import { useLaunchers } from './composables/useLaunchers'
import { useWailsEvent } from './composables/useWailsEvent'
import { comboFromEvent, formatCombo, terminalEscapeCombo, useKeybindings } from './composables/useKeybindings'
import { commands as bindableCommands, launcherActionID } from './keybindings/catalog'
import { setTheme, themeLabels, themes } from './composables/useTheme'
import { useFlowsSession } from './pipeline/composables/useFlowsSession'
import { isEditableTarget, isTerminalTarget } from './lib/isEditableTarget'
import { InstallUpdate, Status as UpdaterStatus } from '../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/updaterservice'
import { Enabled as TerminalModeEnabled } from '../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import { InboxItemFeed } from '../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice'
import type { NotificationActivation, NotificationToast, UpdateInfo } from '../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'
import {
  isApplicationSettingsSection,
  isProfileSettingsSection,
  type ApplicationSettingsSection,
  type ProfileSettingsSection,
} from './router'
import type { SidebarSelection } from './types/feed'
import { kind } from './lib/itemPresentation'

// Only true when Vite is serving in dev mode (under `wails3 dev`). Keeping
// these imports inside this compile-time conditional prevents developer tools
// and their notification implementation from shipping in production bundles.
const devMode = import.meta.env.DEV
const DevBar = devMode ? defineAsyncComponent(() => import('./components/DevBar.vue')) : null
const DevView = devMode ? defineAsyncComponent(() => import('./components/DevView.vue')) : null

// Async so xterm.js stays out of the initial bundle: terminal mode is opt-in
// and the hub must not pay for it at startup.
const TerminalMode = defineAsyncComponent(() => import('./components/TerminalMode.vue'))
// Same reason, and mounted only once the pop-up is first asked for — after
// which it stays mounted, because hiding it must not end the shell inside it.
const PopupTerminal = defineAsyncComponent(() => import('./components/PopupTerminal.vue'))

const {
  status: githubStatus, connected: githubConnected, deviceFlow, card: connectCard, error: connectError, busy: connectBusy,
  startDeviceFlow, useTokenInstead, backToStart, submitToken,
} = useGitHubConnection()

const {
  permission: notificationPermission, requestingPermission, error: notificationError, requestPermission,
} = useNotificationSettings()

const {
  profiles, profilesLoaded, profilesError, activeProfile, activeProfileId, selection, items, sourceIcons, sourceImages, visibleItems, unreadCount, search, loadError,
  selectedId, selectedItem, actions, pendingAction, actionRuns, sessionLaunchAction, sessionLaunchOptions, sessionLaunchBusy, sessionLaunchError, actionInputsAction, actionInputsBusy, actionInputsError, actionRerunConfirmation, actionRerunBusy, actionRerunError, unreadOnly, feedSort, setFeedSort, title, toasts, showToast, dismissToast, clearToasts,
  creatingProfile, createProfileError, renamingProfile, renameProfileError, togglingProfileId, toggleProfileError, deletingProfile, settingProfileImage, profileImageError, loadProfiles, createProfile, seedStarterFlow, renameProfile, setProfileEnabled, deleteProfile, setProfileImage, clearProfileImage,
  visibleArchivedItems, archivedExpanded, archivedCount, toggleArchivedSection, trashFilter, setTrashFilter,
  reorderFeeds, selectProfile, defaultSelection, selectSidebar, selectItem, openActionRun, selectNext, selectPrev,
  toggleUnread, markItemUnread, markingAllRead, markAllRead, unreadInScope, toggleArchive, toggleIgnored, loadEvents, refresh, invokeAction, cancelActionRerun, confirmActionRerun, cancelSessionLaunch, submitSessionLaunch, cancelActionInputs, submitActionInputs, notWired, openUrl, openItemInBrowser, openSelectedInBrowser, copyItemLink, copyItemContents, runItemAction, hideWindow,
} = useFeedState()

// The feed-item kinds currently in the system — what the actions editor
// autocompletes and validates "applies to" against. kind() never returns
// empty (untyped items report DEFAULT_ITEM_KIND), so untyped items are
// offered as a target like any other kind.
const knownFeedTypes = computed(() => [...new Set(items.value.map((item) => kind(item)))].sort((a, b) => a.localeCompare(b)))


const selectedEvents = ref([] as Awaited<ReturnType<typeof loadEvents>>)
let selectedEventsSeq = 0
watch(selectedItem, async (item) => {
  const seq = ++selectedEventsSeq
  selectedEvents.value = []
  if (!item) return
  try {
    const events = await loadEvents(item.id)
    // A slower request for the previously selected item must never replace the
    // timeline for the item currently visible in the detail pane.
    if (seq === selectedEventsSeq && selectedItem.value?.id === item.id) selectedEvents.value = events
  } catch (error) {
    if (seq === selectedEventsSeq && selectedItem.value?.id === item.id) {
      console.warn('Unable to load inbox item events', error)
    }
  }
}, { immediate: true })

// ── Flows session (hc-8ft4yhm6) ──────────────────────────────────────────────
// A profile IS a flow, so the flows canvas is a per-profile sub-view: it swaps
// the sidebar+main region while the spaces rail and titlebar stay mounted (so
// the user is never stranded — see the template). Reached from the sidebar's
// "Flows" pill / "Edit flow" footer and the ⌘K command; exited by selecting a
// profile in the spaces rail or the ⌘K "Back to feed" command.
//
// The session (useFlowsSession) is a module singleton shared with
// FlowsView.vue: it owns the pipeline editor state — which flow is being
// edited, its dirty draft, the flow listing — nothing more. Execution is the
// Go engine's (runtime.Engine) alone, and it keeps every enabled flow running
// with the canvas closed or another profile selected; that is why feeds keep
// updating regardless of what this session is doing. App.vue is the first
// caller, which makes the session app-lived rather than dependent on
// FlowsView mounting/unmounting.
const session = useFlowsSession()

// ── Route-driven navigation ────────────────────────────────────────────────
// The Wails webview uses hash history: routes survive asset:// hosting and
// native/browser back and forward controls traverse the same page stack. The
// shell (title bar + profile rail) stays mounted while this route selects its
// main page.
const router = useRouter()
const route = useRoute()
const flowsActive = computed(() => route.name === 'flows')
const activityActive = computed(() => route.name === 'activity')
const devActive = computed(() => devMode && route.name === 'dev')
const applicationSettingsActive = computed(() => route.name === 'application-settings')
const profileSettingsActive = computed(() => route.name === 'profile-settings')
// Resolved against router.ts's section lists rather than a whitelist repeated
// here: the route already rejects an unknown :section, so anything that
// reaches this point and is not recognized is the absent-param case.
const applicationSettingsSection = computed<ApplicationSettingsSection>(() =>
  isApplicationSettingsSection(route.params.section) ? route.params.section : 'appearance',
)
const profileSettingsSection = computed<ProfileSettingsSection>(() =>
  isProfileSettingsSection(route.params.section) ? route.params.section : 'general',
)
const canGoBack = computed(() => {
  void route.fullPath
  return router.options.history.state.back !== null
})
const canGoForward = computed(() => {
  void route.fullPath
  return router.options.history.state.forward !== null
})

// Keep the selected backend profile and flow draft aligned with route params.
// Missing profile params occur only on the first /feed load; canonicalize that
// entry once profiles arrive so future history entries are self-contained.
watch(activeProfileId, (id) => {
  session.bindActiveFlow(id || undefined)
  const routeNeedsProfile = route.name === 'feed' || route.name === 'flows' || route.name === 'profile-settings'
  if (id && routeNeedsProfile && !route.params.profileId) {
    void router.replace({ name: route.name, params: { ...route.params, profileId: id }, query: route.query })
  }
})

let feedRouteSync = 0
watch([profilesLoaded, () => route.fullPath], async ([loaded]) => {
  if (!loaded) return
  const sync = ++feedRouteSync
  const rawProfileId = route.params.profileId
  if (typeof rawProfileId !== 'string') return
  if (!profiles.value.some((profile) => profile.id === rawProfileId)) {
    if (activeProfileId.value) void router.replace({ name: 'feed', params: { profileId: activeProfileId.value } })
    return
  }
  if (rawProfileId !== activeProfileId.value) await selectProfile(rawProfileId)
  if (sync !== feedRouteSync || route.name !== 'feed') return

  const rawFeedId = route.query.feed
  const feedId = typeof rawFeedId === 'string' && activeProfile.value?.feeds.some((feed) => feed.id === rawFeedId)
    ? rawFeedId
    : null
  const wantsUnread = route.query.unread === '1'
  if (feedId) await selectSidebar({ type: 'feed', feedId })
  else if (route.query.view === 'trash') await selectSidebar({ type: 'trash' })
  // A bare feed route means "the workspace default": last-selected feed,
  // else the first feed in sidebar order. Defaults are never persisted as
  // the remembered selection.
  else await selectSidebar(defaultSelection(rawProfileId), { persist: false })

  // Unread narrows whichever feed the route selected.
  // selectSidebar clears the flag, so apply it after loading that list.
  if (wantsUnread && !unreadOnly.value) await toggleUnread()

  // ?item reveals one row in whichever list the route just loaded. It is how
  // a clicked notification lands on its item, so the destination is a normal
  // route: back/forward traverse it like any other navigation.
  const wantedItem = Number(route.query.item)
  if (!Number.isSafeInteger(wantedItem) || wantedItem <= 0) return
  // An archived row lives in the lazily loaded section below the list, so
  // expand it before giving up on finding the item.
  if (!items.value.some((item) => item.id === wantedItem) && !archivedExpanded.value) {
    await toggleArchivedSection()
    if (sync !== feedRouteSync || route.name !== 'feed') return
  }
  await selectItem(wantedItem)
}, { immediate: true })

watch([() => route.name, () => route.query.node], ([name, rawNode]) => {
  if (name === 'flows') session.openFlows(typeof rawNode === 'string' ? rawNode : undefined)
  else session.exitFlows()
}, { immediate: true })

// A router guard protects dirty flow drafts for every navigation source,
// including native mouse/browser Back — not only the app's own buttons.
type PendingNavigation = { to: string }
const pendingNavigation = ref<PendingNavigation | null>(null)
const unsavedChangesBusy = ref(false)
let allowGuardedNavigation = false
const removeNavigationGuard = router.beforeEach((to, from) => {
  const switchesProfile = typeof to.params.profileId === 'string' && to.params.profileId !== activeProfileId.value
  const leavesDirtyFlow = from.name === 'flows' && (
    to.name !== 'flows' || to.params.profileId !== from.params.profileId
  )
  if (!allowGuardedNavigation && (leavesDirtyFlow || switchesProfile) && session.dirty.value) {
    pendingNavigation.value = { to: to.fullPath }
    return false
  }
})
onUnmounted(removeNavigationGuard)

function cancelPendingNavigation(): void {
  pendingNavigation.value = null
}

async function finishPendingNavigation(): Promise<void> {
  const pending = pendingNavigation.value
  if (!pending) return
  pendingNavigation.value = null
  allowGuardedNavigation = true
  try {
    await router.push(pending.to)
  } finally {
    allowGuardedNavigation = false
  }
}

async function deployPendingNavigation(): Promise<void> {
  if (!pendingNavigation.value) return
  unsavedChangesBusy.value = true
  await session.deploy()
  unsavedChangesBusy.value = false
  if (session.dirty.value) return
  await finishPendingNavigation()
}

async function discardPendingNavigation(): Promise<void> {
  if (!pendingNavigation.value) return
  unsavedChangesBusy.value = true
  await session.discardDraft()
  unsavedChangesBusy.value = false
  if (session.dirty.value) return
  await finishPendingNavigation()
}

function openFeed(profileId = activeProfileId.value): void {
  void router.push({ name: 'feed', params: profileId ? { profileId } : {} })
}

function navigateSidebar(nextSelection: SidebarSelection): void {
  if (!activeProfileId.value) return
  const query = nextSelection.type === 'feed'
    ? { feed: nextSelection.feedId }
    : { view: 'trash' }
  void router.push({ name: 'feed', params: { profileId: activeProfileId.value }, query })
}

function navigateUnreadFilter(value: boolean): void {
  if (!activeProfileId.value) return
  const query: Record<string, string> = {}
  if (selection.value.type === 'feed') query.feed = selection.value.feedId
  else query.view = 'trash'
  if (value) query.unread = '1'
  void router.push({ name: 'feed', params: { profileId: activeProfileId.value }, query })
}

function navigateUnreadToggle(): void {
  navigateUnreadFilter(!unreadOnly.value)
}

function openFlows(focusNodeId?: string): void {
  if (!activeProfileId.value) return
  // Update immediately for command-palette and canvas focus feedback; the
  // route watcher keeps this state aligned during back/forward traversal.
  session.openFlows(focusNodeId)
  void router.push({
    name: 'flows',
    params: { profileId: activeProfileId.value },
    query: focusNodeId ? { node: focusNodeId } : {},
  })
}

function requestExitFlows(): void {
  openFeed()
}

async function requestSelectProfile(id: string): Promise<void> {
  // Returning from the canvas to the already-active profile does not change
  // the route profile id, so the route watcher will not reload its feeds.
  // Refresh explicitly after a clean deploy so the sidebar cannot retain the
  // pre-deploy flow snapshot if flows:updated races the filesystem watcher.
  if (id === activeProfileId.value && !session.dirty.value) await selectProfile(id)
  openFeed(id)
}

function requestOpenActionsSettings(): void {
  void router.push({ name: 'application-settings', params: { section: 'actions' } })
}

function requestOpenSettings(page: 'application' | 'profile'): void {
  if (page === 'application') void router.push({ name: 'application-settings' })
  else if (activeProfileId.value) void router.push({ name: 'profile-settings', params: { profileId: activeProfileId.value } })
}

// ── Activity (6d) ─────────────────────────────────────────────────────────────
// App-global audit log. The titlebar's Activity link replaces the old "polling
// github" indicator; unseenActivity drives its dot.
const { unseenCount: unseenActivity } = useActivity()
const { activeJobs, hasActive: jobsActive } = useJobs()

function openActivity(): void {
  void router.push({ name: 'activity' })
}

async function openJobRun(commandID: number): Promise<void> {
  const job = activeJobs.value.find((candidate) => candidate.commandId === commandID)
  if (!job || !activeProfileId.value) return
  await router.push({ name: 'feed', params: { profileId: activeProfileId.value } })
  if (route.name !== 'feed') return
  // The route watcher applied the workspace default selection; the run opens
  // only if its item is present in that list.
  await openActionRun(Number(job.target), job.actionId, commandID)
}

// ── Desktop self-update ───────────────────────────────────────────────────────
// The UpdaterService checks the configured release-channel manifest for a
// newer desktop release in the background (when enabled) and emits update:available. We seed initial state
// via Status() on mount and keep it current through the subscription, so the
// title-bar chip appears without waiting for the next poll. Clicking it
// downloads + relaunches into the new version.
const updateInfo = ref<UpdateInfo | null>(null)
const updateAvailable = computed(() => updateInfo.value?.available ?? false)
const updateLatestVersion = computed(() => updateInfo.value?.latestVersion ?? '')
const installingUpdate = ref(false)
const updateConfirmOpen = ref(false)
const updateInstallError = ref('')

function openUpdate(): void {
  if (installingUpdate.value) return
  updateInstallError.value = ''
  updateConfirmOpen.value = true
}

function cancelUpdate(): void {
  if (installingUpdate.value) return
  updateConfirmOpen.value = false
  updateInstallError.value = ''
}

async function confirmUpdate(): Promise<void> {
  if (installingUpdate.value) return
  installingUpdate.value = true
  updateInstallError.value = ''
  try {
    await InstallUpdate()
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error)
    console.error('Update install failed', error)
    updateInstallError.value = detail
    showToast('Could not install the update', {
      severity: 'error',
      body: detail,
      duration: 10_000,
    })
    installingUpdate.value = false
  }
}

// ── Mark all as read ─────────────────────────────────────────────────────────
// Clearing one feed is scoped, visible in the sidebar, and the thing the user
// just asked for, so it runs straight away. The workspace variant reaches every
// feed at once with no undo, so it confirms and names the count first.
const markWorkspaceReadOpen = ref(false)
const workspaceUnreadCount = computed(() => unreadInScope(null))

function markFeedRead(feedId: string): void {
  void markAllRead(feedId)
}

function markSelectedFeedRead(): void {
  // Trash carries no unread semantics, so there is nothing here to clear.
  if (selection.value.type !== 'feed') return
  markFeedRead(selection.value.feedId)
}

function requestMarkWorkspaceRead(): void {
  if (markingAllRead.value) return
  markWorkspaceReadOpen.value = true
}

async function confirmMarkWorkspaceRead(): Promise<void> {
  await markAllRead(null)
  markWorkspaceReadOpen.value = false
}

function closeSettings(): void {
  openFeed()
}

function selectApplicationSettingsSection(section: ApplicationSettingsSection): void {
  void router.push({ name: 'application-settings', params: { section } })
}

function selectProfileSettingsSection(section: ProfileSettingsSection): void {
  if (!activeProfileId.value) return
  void router.push({ name: 'profile-settings', params: { profileId: activeProfileId.value, section } })
}

// ── Titlebar error chip (8d) ──────────────────────────────────────────────────
// Sourced from the always-on session (not FlowsView), so the chip renders and
// deep-links correctly even with the canvas closed.
const errorNodeIds = computed(() =>
  (session.activeFlow.value?.nodes ?? [])
    .filter((node) => session.latestRunByNode.value.get(node.id)?.ok === false)
    .map((node) => node.id),
)
const errorCount = computed(() => errorNodeIds.value.length)
const firstErrorNodeId = computed(() => errorNodeIds.value[0])

function openErrorNode(): void {
  if (firstErrorNodeId.value) openFlows(firstErrorNodeId.value)
}

// A clicked notification arrives with the workspace and item it was sent
// about (see the notify node). The window is already raised by the time this
// fires; routing to a feed route that reveals the item is all that is left.
// An item id of 0 means the notification had none — land on the workspace.
async function revealNotification(activation: NotificationActivation): Promise<void> {
  const profileId = activation.profileId
  if (!profileId) return
  const itemId = Number(activation.itemId ?? 0)
  if (!Number.isSafeInteger(itemId) || itemId <= 0) {
    openFeed(profileId)
    return
  }
  let feedId = ''
  try {
    feedId = (await InboxItemFeed(profileId, itemId)) ?? ''
  } catch (error) {
    console.warn('Unable to locate the notified item', error)
  }
  const query: Record<string, string> = { item: String(itemId) }
  if (feedId) query.feed = feedId
  else query.view = 'trash'
  void router.push({ name: 'feed', params: { profileId }, query })
}

let unsubscribeInbox: (() => void) | undefined
let unsubscribeFlowsUpdated: (() => void) | undefined
let unsubscribeUpdate: (() => void) | undefined
let unsubscribeNotification: (() => void) | undefined
let unsubscribeNotificationToast: (() => void) | undefined

// Go speaks the notify vocabulary (info/success/warning/error); the toast
// stack speaks its own. Anything unrecognized reads as info rather than
// being dropped — the message still matters.
function toastSeverity(severity: string): 'info' | 'success' | 'warning' | 'error' {
  const known = ['info', 'success', 'warning', 'error'] as const
  return known.includes(severity as (typeof known)[number]) ? severity as (typeof known)[number] : 'info'
}
onMounted(() => {
  // The Go flow engine commits before it announces, so this is the moment
  // membership claims and inbox items are readable — not log:appended, which
  // only says a source observed something that may route nowhere at all.
  unsubscribeInbox = Events.On('inbox:updated', () => { void refresh() })
  // The app owns this subscription, rather than FlowsView, because the flow
  // listing feeds the sidebar whether or not the canvas is open. The session
  // keeps an unsaved editor draft private while refreshing the rest.
  unsubscribeFlowsUpdated = Events.On('flows:updated', () => { void session.reloadFlows() })
  // Seed the update chip from the last cached check, then react to background
  // checks. The event payload is the same UpdateInfo shape Status() returns.
  void UpdaterStatus().then((status) => { updateInfo.value = status }).catch((error) => {
    console.debug('Updater status unavailable', error)
  })
  unsubscribeUpdate = Events.On('update:available', (event: { data: UpdateInfo | UpdateInfo[] }) => {
    const payload = Array.isArray(event.data) ? event.data[0] : event.data
    if (payload) updateInfo.value = payload
  })
  unsubscribeNotification = Events.On('notification:activated', (event: { data: NotificationActivation | NotificationActivation[] }) => {
    const payload = Array.isArray(event.data) ? event.data[0] : event.data
    if (payload) void revealNotification(payload)
  })
  // A flow notification the user chose to receive in-app rather than as an OS
  // banner (Settings -> Notifications -> Delivery). Go has already applied the
  // kill switch and picked this channel; the toast stack is the same one every
  // other in-app notification uses.
  unsubscribeNotificationToast = Events.On('notification:toast', (event: { data: NotificationToast | NotificationToast[] }) => {
    const payload = Array.isArray(event.data) ? event.data[0] : event.data
    if (payload?.title) showToast(payload.title, { body: payload.body, severity: toastSeverity(payload.severity) })
  })
})
onUnmounted(() => {
  unsubscribeInbox?.()
  unsubscribeFlowsUpdated?.()
  unsubscribeUpdate?.()
  unsubscribeNotification?.()
  unsubscribeNotificationToast?.()
})

// ── Profile create / delete overlays ─────────────────────────────────────────

const newProfileOpen = ref(false)

function openNewProfile() {
  createProfileError.value = null // a stale failure must not greet the reopen
  newProfileOpen.value = true
}

async function submitNewProfile(name: string) {
  await createProfile(name)
  if (!createProfileError.value) {
    newProfileOpen.value = false
    openFeed(activeProfileId.value)
  }
}

async function submitProfileRename(name: string) {
  if (!activeProfileId.value) return
  await renameProfile(activeProfileId.value, name)
}

async function submitProfileEnabled(enabled: boolean) {
  if (!activeProfileId.value) return
  await setProfileEnabled(activeProfileId.value, enabled)
}

async function submitProfileImage(data: string) {
  if (!activeProfileId.value) return
  await setProfileImage(activeProfileId.value, data)
}

async function submitProfileClearImage() {
  if (!activeProfileId.value) return
  await clearProfileImage(activeProfileId.value)
}

const deleteProfileOpen = ref(false)

function openDeleteProfile() {
  deleteProfileOpen.value = true
}

async function confirmDeleteProfile() {
  if (!activeProfileId.value) return
  const deleted = await deleteProfile(activeProfileId.value)
  if (!deleted) return
  deleteProfileOpen.value = false
  openFeed()
}

// Profiles load at startup regardless of GitHub (useFeedState's onMounted) —
// nothing in the app is gated on being connected. This reload is about
// identity, not availability: a different account must never be shown the
// previous account's data.
watch(() => (githubConnected.value ? githubStatus.value?.login ?? '' : null), (key) => {
  if (key !== null) void loadProfiles()
})

// ── First run ────────────────────────────────────────────────────────────────
// create workspace -> connect GitHub -> feed. The workspace goes first because
// it is the one thing that exists without a credential; connecting is the
// expected next step but can be skipped past a warning, and skipping lands on
// a feed whose empty state points at Integrations.

// Step 1: no workspace exists yet. This is also where deleting the last
// workspace lands.
const needsWorkspace = computed(() => profilesLoaded.value && profiles.value.length === 0)

// Step 2. It is the tail of one continuous first run rather than a state the
// app persists: set when the first workspace is created with nothing
// connected, cleared by connecting or skipping. Disconnecting later never
// sets it — Settings ▸ Integrations is where that is repaired.
const firstRunConnect = ref(false)

// Step 3: the OS notification grant. Like firstRunConnect it is the tail of
// one first run, not persisted state — set when the connect step resolves and
// cleared once the user grants, denies, or skips. Requesting it here is the
// only place onboarding pops the OS prompt; a returning user whose permission
// is already resolved never sees this step (advanceToPermissions gates on it).
const firstRunPermissions = ref(false)
const onboardingActive = computed(() => needsWorkspace.value || firstRunConnect.value || firstRunPermissions.value)

// Move off the connect step onto the permissions step, unless the OS decision
// is already made — a grant or a denial has nothing left to ask, so first run
// ends and the feed takes over.
function advanceToPermissions(): void {
  firstRunPermissions.value = notificationPermission.value === 'not-requested'
}

function skipConnectStep(): void {
  firstRunConnect.value = false
  advanceToPermissions()
}

async function submitOnboardingWorkspace(name: string): Promise<void> {
  // Claim the connect step before creating: the profiles list gains the new
  // workspace partway through createProfile, and without this the feed would
  // render for a frame in between.
  firstRunConnect.value = !githubConnected.value
  if (!(await createProfile(name))) firstRunConnect.value = false
}

// Connecting during first run seeds the workspace made a step earlier. It was
// made empty because a source node names the account it fetches as and there
// was none; this is the moment there is one. The connect card stays up until
// the seed lands, so the feed is never rendered sourceless on the way through.
watch(githubConnected, async (connected) => {
  if (!connected || !firstRunConnect.value) return
  const profileId = activeProfileId.value
  try {
    if (profileId) await seedStarterFlow(profileId)
  } catch (error) {
    console.warn('Unable to seed the starter flow', error)
    showToast('Starter feeds were not added', {
      body: 'This workspace has no sources yet — add one in the flow editor.',
      severity: 'error',
    })
  } finally {
    firstRunConnect.value = false
    advanceToPermissions()
  }
})

// ── App mode ─────────────────────────────────────────────────────────────────
// Hub is the feed/flows/settings app; Terminal takes the whole frame under the
// title bar. Terminal is a route (/terminal/:slug?), so the title-bar controls
// stay live inside it — Activity, back/forward — and history traversal
// restores the attached session. A relaunch still lands on the hub (the
// webview loads with no hash), so no relaunch attaches a tmux control client
// unprompted; re-entering the mode is what resumes the last session.
const mode = computed<'hub' | 'terminal'>(() => (route.name === 'terminal' ? 'terminal' : 'hub'))
const terminalActive = computed(() => mode.value === 'terminal' && !onboardingActive.value)

// Where the Hub button lands: the last hub route, so toggling into the
// terminal and back is not a trip to the default feed.
let lastHubPath = ''
watch(() => route.fullPath, (path) => {
  if (route.name && route.name !== 'terminal') lastHubPath = path
}, { immediate: true })

// Terminal mode ships dark (experimental.terminal, ADR 0037): until the probe
// answers true, the toggle into it does not render at all. Availability is a
// separate axis — an enabled-but-unavailable terminal explains itself inside
// the mode.
const terminalEnabled = ref(false)
onMounted(() => {
  void TerminalModeEnabled().then((enabled) => { terminalEnabled.value = enabled }).catch((error) => {
    console.debug('Terminal enablement unavailable', error)
  })
})

function setMode(next: 'hub' | 'terminal'): void {
  if (next === mode.value) return
  if (next === 'terminal') void router.push({ name: 'terminal' })
  else void router.push(lastHubPath || { name: 'feed' })
}

// ── Layout chrome ─────────────────────────────────────────────────────────────
// Each mode remembers its own left panel while the feed detail preview remains
// feed-only. The title-bar toggle follows whichever mode owns the current panel.
const feedSidebarCollapsed = useStorage('hive.panel.sidebar.collapsed', false)
const terminalSidebarCollapsed = useStorage('hive.panel.terminal.sidebar.collapsed', false)
const previewCollapsed = useStorage('hive.panel.detailpane.collapsed', false)
const feedViewActive = computed(() =>
  !onboardingActive.value && !terminalActive.value &&
  !applicationSettingsActive.value && !profileSettingsActive.value &&
  !flowsActive.value && !activityActive.value && !devActive.value &&
  !!activeProfile.value,
)
const sidebarCollapsed = computed(() =>
  terminalActive.value ? terminalSidebarCollapsed.value : feedSidebarCollapsed.value,
)
const canToggleSidebar = computed(() => terminalActive.value || feedViewActive.value)

function toggleSidebar(): void {
  const collapsed = terminalActive.value ? terminalSidebarCollapsed : feedSidebarCollapsed
  collapsed.value = !collapsed.value
}

function togglePreview(): void {
  previewCollapsed.value = !previewCollapsed.value
}

// We draw our own hidden-inset title bar, so the native double-click-to-zoom
// gesture has to be re-implemented. Guarded for the non-Wails test/browser
// context, matching hideWindow's posture.
async function toggleMaximise(): Promise<void> {
  try {
    if (typeof Window?.ToggleMaximise === 'function') await Window.ToggleMaximise()
  } catch (error) {
    console.debug('Window maximise is unavailable outside Wails', error)
  }
}

// ── Command palette ──────────────────────────────────────────────────────────

const { open: paletteOpen, toggle: togglePalette } = useCommandPalette()
const { open: reportDialogOpen, openDialog: openReportDialog } = useReportDialog()
const {
  open: newSessionOpen, options: newSessionOptions, initial: newSessionInitial, busy: newSessionBusy, error: newSessionError,
  openBlank: openNewSession, openFromItem: openNewSessionFromItem, cancel: cancelNewSession, submit: submitNewSession,
} = useNewSession()
const kb = useKeybindings()

// The URL is the attach state, so it is also the answer to "which session is on
// screen" — the pop-up terminal, the launchers, and a new session all follow it.
const onScreenSessionSlug = computed(() =>
  (route.name === 'terminal' && typeof route.params.slug === 'string' ? route.params.slug : ''))

// The pop-up terminal opens in the checkout of whichever session is on screen,
// and in the user's home when none is (ADR 0048). The panel is mounted on first
// use and stays mounted: hiding it is a view change, not the end of the shell.
const popupTerminal = usePopupTerminal()
const popupTerminalMounted = ref(false)

function togglePopupTerminal(): void {
  popupTerminalMounted.value = true
  popupTerminal.toggle({ sessionSlug: onScreenSessionSlug.value || undefined })
}

// A launcher is the pop-up opened straight into a program. It follows the
// session on screen exactly as the bare shell does — that is what makes one
// chord mean "lazygit here" wherever you are — unless the launcher pins itself
// to a directory, which the core decides from the catalog.
function toggleLauncher(actionID: string): void {
  popupTerminalMounted.value = true
  popupTerminal.toggle({ launcher: actionID, sessionSlug: onScreenSessionSlug.value || undefined })
}

// The launchers are read here rather than by the panel: they are commands in
// the palette and the keymap whether or not a pop-up has ever been opened, so
// they have to be known before the first one is invoked.
const launchers = useLaunchers()
onMounted(() => { void launchers.refresh() })
useWailsEvent('actions:updated', () => { void launchers.refresh() })

// One handler per bindable command id. Both the keydown dispatcher and the
// command palette run through this map, so each command has a single
// implementation and the palette can show its live shortcut.
const runMap: Record<string, () => void | Promise<void>> = {
  'feed.next': selectNext,
  'feed.prev': selectPrev,
  'feed.open-in-browser': openSelectedInBrowser,
  'feed.toggle-unread': navigateUnreadToggle,
  'feed.toggle-preview': togglePreview,
  'feed.refresh': refresh,
  'feed.toggle-archive': async () => { if (selectedItem.value) await toggleArchive(selectedItem.value) },
  'feed.mark-unread': async () => { if (selectedItem.value) await markItemUnread(selectedItem.value, true) },
  'feed.mark-all-read': markSelectedFeedRead,
  'feed.mark-workspace-read': requestMarkWorkspaceRead,
  'palette.toggle': togglePalette,
  'report.open': openReportDialog,
  'terminal.popup.toggle': togglePopupTerminal,
  'session.new': () => openNewSession(sessionRepository(onScreenSessionSlug.value)),
  'window.hide': hideWindow,
}

// Resolves a command id to its implementation. Launchers are not in runMap:
// they come from actions.yml, so there is one implementation parameterised by
// the action id rather than an entry per launcher.
function runCommand(id: string): void {
  const launcher = launcherActionID(id)
  if (launcher !== null) {
    toggleLauncher(launcher)
    return
  }
  void runMap[id]?.()
}

const catalogById = computed(() => new Map(bindableCommands.value.map((command) => [command.id, command])))

// The feed only accepts bare navigation keys when it is actually the on-screen
// view (matches the condition under which <FeedList> renders below).
const feedNavActive = computed(() =>
  route.name === 'feed' && !onboardingActive.value && !terminalActive.value && !!activeProfile.value,
)

// While an overlay owns the screen, only the palette toggle stays live.
const anyOverlayOpen = computed(() =>
  paletteOpen.value || reportDialogOpen.value || newProfileOpen.value || deleteProfileOpen.value || markWorkspaceReadOpen.value || newSessionOpen.value || !!sessionLaunchAction.value || !!actionInputsAction.value || !!pendingNavigation.value,
)

// Seed commands — reactive getter so they update when profiles/flows load
useCommands(computed(() => {
  const cmds: Command[] = []

  // Bindable app commands (nav, refresh, …) and the configured launchers, each
  // with its live shortcut hint.
  for (const command of bindableCommands.value) {
    if (command.paletteHidden) continue
    cmds.push({
      id: command.id,
      title: command.title,
      group: command.group,
      keywords: command.keywords,
      icon: command.icon,
      hint: formatCombo(kb.bindings.value[command.id]?.[0] ?? ''),
      run: () => runCommand(command.id),
    })
  }

  // Profiles
  for (const p of profiles.value) {
    cmds.push({
      id: `profile:${p.id}`,
      title: `Switch to profile: ${p.name}`,
      group: 'Profiles',
      icon: IconLayoutGrid,
      run: () => requestSelectProfile(p.id),
    })
  }

  // Feeds — All items always first, then individual feeds
  const profileName = activeProfile.value?.name

  cmds.push({
    id: 'view:trash',
    title: 'Open Trash',
    group: 'Feeds',
    icon: IconList,
    hint: profileName,
    run: () => navigateSidebar({ type: 'trash' }),
  })

  for (const f of activeProfile.value?.feeds ?? []) {
    cmds.push({
      id: `feed:${f.id}`,
      title: `Select feed: ${f.name}`,
      group: 'Feeds',
      icon: IconRss,
      hint: profileName,
      run: () => navigateSidebar({ type: 'feed', feedId: f.id }),
    })
  }

  cmds.push({
    id: 'profile:new',
    title: 'New profile…',
    group: 'Profiles',
    keywords: ['workspace', 'create'],
    run: openNewProfile,
  })

  // View — enter/exit the flows canvas for the active profile.
  cmds.push({
    id: 'flow:edit',
    title: flowsActive.value ? 'Back to feed' : 'Edit flow…',
    group: 'View',
    keywords: ['flows', 'pipeline', 'nodes', 'canvas', 'editor'],
    icon: IconWorkflow,
    run: () => { flowsActive.value ? requestExitFlows() : openFlows() },
  })

  // Jump to any node in the active flow by name (8d) — opens the canvas
  // focused/centered on that node, same as "Reveal in flow" from the sidebar.
  for (const node of session.activeFlow.value?.nodes ?? []) {
    cmds.push({
      id: `flow:node:${node.id}`,
      title: `Jump to node: ${node.name || node.type}`,
      group: 'Flow',
      keywords: ['flows', 'node', 'canvas', 'reveal'],
      icon: IconShare2,
      run: () => openFlows(node.id),
    })
  }

  // Themes
  for (const t of themes) {
    cmds.push({
      id: `theme:${t}`,
      title: `Theme: ${themeLabels[t]}`,
      group: 'Theme',
      keywords: ['theme', 'appearance', t],
      icon: IconPalette,
      run: () => setTheme(t),
    })
  }

  return cmds
}))

// ── Global input navigation ──────────────────────────────────────────────────
// Resolves a keydown against the configurable keymap and runs the matched
// command. Bare (modifier-less) keys are ignored while typing; feed commands
// only fire on the feed; overlays suppress everything but the palette toggle.

function onGlobalKeydown(e: KeyboardEvent): void {
  // The exceptions to the rule below, which hands a focused terminal every key.
  // The combo that opens a pop-up has to be able to close it, and by then a
  // terminal has focus; a launcher's chord is one of those for the same reason.
  // The palette is the third, because it is how you get back out of a pane. An
  // overlay still suppresses all of them, as it does every global command.
  if (!kb.recording.value && !anyOverlayOpen.value) {
    const id = kb.resolve(comboFromEvent(e) ?? '')
    if (id === 'terminal.popup.toggle' || (id && launcherActionID(id) !== null)) {
      e.preventDefault()
      runCommand(id)
      return
    }
    // The palette is the way back out of a pane, so it fires over one too — but
    // only on modifiers a terminal cannot use, which is what terminalEscapeCombo
    // answers. A bare Ctrl+K stays with the pane; it is readline's
    // kill-to-end-of-line.
    if (isTerminalTarget(e.target) && kb.resolve(terminalEscapeCombo(e) ?? '') === 'palette.toggle') {
      e.preventDefault()
      togglePalette()
      return
    }
  }

  // A focused terminal owns every key, modifiers included, so tmux prefixes
  // reach the pane instead of firing a Hive shortcut.
  if (isTerminalTarget(e.target)) return

  // WebKit can treat an unhandled Backspace as browser Back. Suppress that
  // default outside editors while still allowing components such as the flow
  // canvas to use Backspace for their own actions.
  if (e.key === 'Backspace' && !isEditableTarget(e.target)) e.preventDefault()

  if (kb.recording.value) return // the settings editor is capturing this key
  const combo = comboFromEvent(e)
  if (!combo) return
  const id = kb.resolve(combo)
  if (!id) return
  const command = catalogById.value.get(id)
  if (!command) return

  const mods = combo.split('+')
  const hasModifier = mods.includes('mod') || mods.includes('ctrl') || mods.includes('alt')
  if (isEditableTarget(e.target) && !hasModifier) return

  if (anyOverlayOpen.value && id !== 'palette.toggle') return
  if (command.context === 'feed' && !feedNavActive.value) return

  e.preventDefault()
  runCommand(id)
}

function isHistoryMouseButton(e: MouseEvent): boolean {
  return e.button === 3 || e.button === 4
}

// MouseEvent buttons 3 and 4 are conventionally Browser Back and Browser
// Forward. Handle them through Vue Router so they use the same dirty-flow
// guard as the title-bar controls. Cancel both press and auxiliary-click
// defaults because WebViews differ on which event performs native navigation.
function preventNativeMouseHistory(e: MouseEvent): void {
  if (isHistoryMouseButton(e)) e.preventDefault()
}

function onGlobalMouseUp(e: MouseEvent): void {
  if (!isHistoryMouseButton(e)) return
  e.preventDefault()
  if (e.button === 3) router.back()
  else router.forward()
}

onMounted(() => {
  window.addEventListener('keydown', onGlobalKeydown)
  window.addEventListener('mousedown', preventNativeMouseHistory)
  window.addEventListener('mouseup', onGlobalMouseUp)
  window.addEventListener('auxclick', preventNativeMouseHistory)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  window.removeEventListener('mousedown', preventNativeMouseHistory)
  window.removeEventListener('mouseup', onGlobalMouseUp)
  window.removeEventListener('auxclick', preventNativeMouseHistory)
})
</script>

<template>
  <main class="h-screen w-screen overflow-hidden bg-app text-text">
    <div class="flex h-full min-h-0 flex-col overflow-hidden">
      <TitleBar
        :profile-name="onboardingActive ? undefined : activeProfile?.name ?? 'Loading'"
        :mode="mode"
        :terminal-enabled="terminalEnabled"
        :activity-active="activityActive"
        :error-count="errorCount"
        :unseen-activity="unseenActivity"
        :jobs-active="jobsActive"
        :active-jobs="activeJobs"
        :update-available="updateAvailable"
        :update-installing="installingUpdate"
        :latest-version="updateLatestVersion"
        :can-go-back="canGoBack"
        :can-go-forward="canGoForward"
        :sidebar-collapsed="sidebarCollapsed"
        :can-toggle-sidebar="canToggleSidebar"
        :preview-collapsed="previewCollapsed"
        :can-toggle-preview="feedViewActive"
        @set-mode="setMode"
        @back="router.back()"
        @forward="router.forward()"
        @open-error-node="openErrorNode"
        @open-activity="openActivity"
        @open-job-run="openJobRun"
        @open-update="openUpdate"
        @toggle-sidebar="toggleSidebar"
        @toggle-preview="togglePreview"
        @open-palette="togglePalette"
        @open-report="openReportDialog"
        @toggle-maximise="toggleMaximise"
      />
      <!-- Hold an empty frame until the workspaces resolve so a returning user
           never sees onboarding flash by. A load failure falls through to the
           shell below, which renders the error with a retry. -->
      <div v-if="!profilesLoaded && !profilesError" class="flex min-h-0 flex-1 items-center justify-center font-mono text-xs text-text-4">Loading…</div>
      <OnboardingScreen
        v-else-if="onboardingActive"
        :card="needsWorkspace ? 'workspace' : firstRunConnect ? connectCard : 'permissions'"
        :device-flow="deviceFlow"
        :error="needsWorkspace ? createProfileError : firstRunConnect ? connectError : notificationError"
        :busy="needsWorkspace ? creatingProfile : firstRunConnect ? connectBusy : requestingPermission"
        :permission="notificationPermission"
        @start-device-flow="startDeviceFlow"
        @use-token-instead="useTokenInstead"
        @back-to-start="backToStart"
        @submit-token="submitToken"
        @create-workspace="submitOnboardingWorkspace"
        @skip-connect="skipConnectStep"
        @request-permission="requestPermission"
        @finish-permissions="firstRunPermissions = false"
      />
      <!-- Terminal mode takes the whole frame under the title bar, spaces rail
           included: nothing in it is workspace-scoped, and the always-live mode
           toggle is the way back. -->
      <TerminalMode v-else-if="terminalActive" :sidebar-collapsed="terminalSidebarCollapsed" />
      <!-- The spaces rail (ProfileRail) and TitleBar stay mounted across the
           feed<->flows switch; only the sidebar+main region swaps. This is
           what keeps the user from being stranded in the flows canvas — the
           spaces rail is always there to navigate back. -->
      <div v-else class="flex min-h-0 flex-1">
        <ProfileRail
          :profiles="profiles"
          :active-profile-id="activeProfileId"
          @select="requestSelectProfile"
          @add="openNewProfile"
          @open-settings="requestOpenSettings('application')"
        />
        <DevView v-if="devMode && devActive" @close="closeSettings" />
        <SettingsView
          v-else-if="applicationSettingsActive"
          :active-category="applicationSettingsSection"
          :known-feed-types="knownFeedTypes"
          @close="closeSettings"
          @select-category="selectApplicationSettingsSection"
        />
        <ProfileSettingsView
          v-else-if="profileSettingsActive && activeProfile"
          :profile="activeProfile"
          :active-section="profileSettingsSection"
          :renaming="renamingProfile"
          :rename-error="renameProfileError"
          :toggling="togglingProfileId !== null"
          :toggle-error="toggleProfileError"
          :setting-image="settingProfileImage"
          :image-error="profileImageError"
          @close="closeSettings"
          @rename="submitProfileRename"
          @toggle-enabled="submitProfileEnabled"
          @set-image="submitProfileImage"
          @clear-image="submitProfileClearImage"
          @delete="openDeleteProfile"
          @select-section="selectProfileSettingsSection"
        />
        <FlowsView v-else-if="flowsActive" />
        <ActivityView v-else-if="activityActive" @close="closeSettings" />
        <template v-else>
          <SideBar
            v-if="activeProfile && !feedSidebarCollapsed"
            :profile="activeProfile"
            :selection="selection"
            :flows-dirty="session.dirty.value"
            @select="navigateSidebar"
            @open-flows="openFlows()"
            @open-settings="requestOpenSettings('profile')"
            @mark-read="markFeedRead"
            @reorder="(t) => activeProfile && reorderFeeds(activeProfile.id, t)"
          />
          <!-- A workspace created before an account was connected has no
               graph at all, so there is no feed to render. Say what is
               missing and where to fix it rather than showing an empty Trash
               view, which is where a feedless flow otherwise lands.
               `tree` is what tells "this flow has no feed nodes" apart from
               "the feeds have not been read yet": a stub whose feeds a reload
               is still fetching has no tree, and must not flash this. -->
          <div
            v-if="activeProfile?.tree && activeProfile.feeds.length === 0"
            class="flex min-w-0 flex-1 flex-col items-center justify-center gap-3 px-10 text-center"
            data-testid="workspace-empty"
          >
            <div class="text-[13.5px] font-semibold">No sources yet</div>
            <p class="max-w-[400px] text-xs leading-relaxed text-text-3">
              {{ githubConnected
                ? 'This workspace has no feeds. Open the flow editor to wire a source into one.'
                : 'This workspace has no feeds, and no account is connected to fetch as. Connect one under Integrations, then wire a source into a feed.' }}
            </p>
            <div class="mt-1 flex items-center gap-2">
              <button
                v-if="!githubConnected"
                class="cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
                data-testid="workspace-empty-integrations"
                @click="selectApplicationSettingsSection('integrations')"
              >Open Integrations</button>
              <button
                class="cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
                data-testid="workspace-empty-flows"
                @click="openFlows()"
              >Edit flow</button>
            </div>
          </div>
          <section v-else-if="activeProfile" class="flex min-w-0 flex-1">
            <FeedList
              :title="title"
              :visible-items="visibleItems"
              :selected-id="selectedId"
              :unread-only="unreadOnly"
              :unread-count="unreadCount"
              :archived-items="visibleArchivedItems"
              :archived-count="archivedCount"
              :archived-expanded="archivedExpanded"
              :trash="selection.type === 'trash'"
              :trash-filter="trashFilter"
              :search="search"
              :sort="feedSort"
              :load-error="loadError"
              :source-icons="sourceIcons"
              :source-images="sourceImages"
              @select="selectItem"
              @update:search="(value) => (search = value)"
              @set-sort="setFeedSort"
              @set-unread="navigateUnreadFilter"
              @toggle-archived="toggleArchivedSection"
              @set-trash-filter="setTrashFilter"
              @refresh="refresh"
              @mark-all-read="markSelectedFeedRead"
              @item-set-unread="markItemUnread"
              @item-toggle-archive="toggleArchive"
              @item-toggle-ignored="toggleIgnored"
              @item-open-browser="openItemInBrowser"
              @item-copy-link="copyItemLink"
              @item-copy-contents="copyItemContents"
              @item-create-session="openNewSessionFromItem"
              @item-run-action="runItemAction"
            />
            <DetailPane v-if="!previewCollapsed" :item="selectedItem" :events="selectedEvents" :actions="actions" :pending-action="pendingAction" :action-runs="actionRuns" :source-icons="sourceIcons" :source-images="sourceImages" @run-action="invokeAction" @open-browser="openSelectedInBrowser" @open-url="openUrl" @set-unread="(value) => selectedItem && markItemUnread(selectedItem, value)" @toggle-archive="selectedItem && toggleArchive(selectedItem)" @toggle-ignored="selectedItem && toggleIgnored(selectedItem)" @copy-link="selectedItem && copyItemLink(selectedItem)" @copy-contents="selectedItem && copyItemContents(selectedItem)" @create-session="selectedItem && openNewSessionFromItem(selectedItem)" @edit="requestOpenActionsSettings" />
          </section>
          <div v-else class="flex flex-1 flex-col items-center justify-center gap-3 font-mono text-xs text-text-4">
            <template v-if="profilesError">
              <span data-testid="profiles-error">{{ profilesError }}</span>
              <button class="cursor-pointer rounded border border-strong px-3 py-1.5 text-text-2 hover:text-text" @click="loadProfiles">Retry</button>
            </template>
            <span v-else>Loading feed…</span>
          </div>
        </template>
      </div>
      <DevBar v-if="devMode" />
    </div>
    <CreateSessionDialog
      v-if="sessionLaunchAction && sessionLaunchOptions"
      :action-label="sessionLaunchAction.label"
      :options="sessionLaunchOptions"
      :inputs="sessionLaunchAction.inputs ?? []"
      :busy="sessionLaunchBusy"
      :error="sessionLaunchError"
      @close="cancelSessionLaunch"
      @submit="submitSessionLaunch"
    />
    <ActionInputsDialog
      v-if="actionInputsAction"
      :action-label="actionInputsAction.label"
      :inputs="actionInputsAction.inputs ?? []"
      :busy="actionInputsBusy"
      :error="actionInputsError"
      @close="cancelActionInputs"
      @submit="submitActionInputs"
    />
    <NewSessionDialog
      v-if="newSessionOpen && newSessionOptions"
      :options="newSessionOptions"
      :initial="newSessionInitial"
      :busy="newSessionBusy"
      :error="newSessionError"
      @close="cancelNewSession"
      @submit="submitNewSession"
    />
    <ConfirmationDialog
      v-if="updateConfirmOpen"
      title="Install update?"
      :description="`Download Hive ${updateLatestVersion || 'update'} and relaunch the app now?`"
      confirm-label="Install and relaunch"
      :busy="installingUpdate"
      :error="updateInstallError"
      testid="update-confirmation"
      @confirm="confirmUpdate"
      @cancel="cancelUpdate"
    />
    <ConfirmationDialog
      v-if="actionRerunConfirmation"
      title="Run action again?"
      :description="`${actionRerunConfirmation.label} has already run for this item. Run it again?`"
      confirm-label="Run again"
      :busy="actionRerunBusy"
      :error="actionRerunError"
      testid="action-rerun-confirmation"
      @confirm="confirmActionRerun"
      @cancel="cancelActionRerun"
    />
    <ConfirmationDialog
      v-if="markWorkspaceReadOpen"
      title="Mark all feeds as read?"
      :description="workspaceUnreadCount === 1
        ? `Clear the unread item in every feed of ${activeProfile?.name ?? 'this workspace'}. This can't be undone.`
        : `Clear all ${workspaceUnreadCount} unread items in every feed of ${activeProfile?.name ?? 'this workspace'}. This can't be undone.`"
      confirm-label="Mark all as read"
      :busy="markingAllRead"
      testid="mark-workspace-read-confirmation"
      @confirm="confirmMarkWorkspaceRead"
      @cancel="markWorkspaceReadOpen = false"
    />
    <ToastStack :toasts="toasts" @dismiss="dismissToast" @clear-all="clearToasts" />
    <PopupTerminal v-if="popupTerminalMounted" />
    <CommandPalette />
    <ReportProblemDialog v-if="reportDialogOpen" @close="reportDialogOpen = false" />
    <NewProfileModal
      v-if="newProfileOpen"
      :busy="creatingProfile"
      :error="createProfileError"
      @close="newProfileOpen = false"
      @create="submitNewProfile"
    />
    <DeleteProfileModal
      v-if="deleteProfileOpen && activeProfile"
      :profile-name="activeProfile.name"
      :busy="deletingProfile"
      @close="deleteProfileOpen = false"
      @confirm="confirmDeleteProfile"
    />
    <UnsavedFlowChangesModal
      v-if="pendingNavigation"
      :busy="unsavedChangesBusy"
      :error="session.error.value"
      @close="cancelPendingNavigation"
      @deploy="deployPendingNavigation"
      @discard="discardPendingNavigation"
    />
  </main>
</template>
