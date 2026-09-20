import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OnboardingScreen from '../OnboardingScreen.vue'
import { useHiveSetup } from '../../composables/useHiveSetup'

vi.mock('@wailsio/runtime', () => ({
  Browser: {
    OpenURL: vi.fn().mockResolvedValue(undefined),
  },
  Clipboard: { SetText: vi.fn().mockResolvedValue(undefined) },
}))

const hiveMocks = vi.hoisted(() => ({
  Setup: vi.fn(),
  Save: vi.fn(),
  InspectWorkspace: vi.fn(),
  ChooseDirectory: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/hiveconfigservice', () => ({
  Setup: hiveMocks.Setup,
  Save: hiveMocks.Save,
  InspectWorkspace: hiveMocks.InspectWorkspace,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice', () => ({
  ChooseDirectory: hiveMocks.ChooseDirectory,
}))

const AGENTS = [
  { name: 'claude', label: 'Claude Code', skipPermissionFlags: ['--dangerously-skip-permissions'], installed: false },
  { name: 'opencode', label: 'OpenCode', skipPermissionFlags: ['--agent', 'free-permissions-runner'], installed: true },
]

function hiveSetup(over: Record<string, unknown> = {}) {
  return {
    config: {
      path: '/home/u/.config/hive/config.yaml',
      exists: false,
      environmentOverride: false,
      usable: false,
      unreadable: '',
      defaultAgent: '',
      profiles: [],
      workspaces: [],
      ...over,
    },
    agents: AGENTS,
    defaultAgentOverride: '',
  }
}

// A loaded composable, the same object App.vue hands the screen.
async function loadedHive(setup = hiveSetup()) {
  hiveMocks.Setup.mockResolvedValue(setup)
  const hive = useHiveSetup()
  await hive.load()
  await flushPromises()
  return hive
}

function mountScreen(props: Partial<InstanceType<typeof OnboardingScreen>['$props']> = {}) {
  return mount(OnboardingScreen, {
    props: {
      card: 'idle',
      deviceFlow: null,
      error: null,
      busy: false,
      githubConnected: false,
      ...props,
    },
  })
}

describe('OnboardingScreen', () => {
  // The disconnected first-run path has four screens. A step the walk can
  // never make active reads as a step that got skipped.
  it('lists exactly the steps onboarding has', () => {
    const wrapper = mountScreen()
    expect(wrapper.findAll('ol li').map((li) => li.text())).toHaveLength(4)
    expect(wrapper.text()).toContain('Set up your agent and code')
    expect(wrapper.text()).toContain('Create your first profile')
    expect(wrapper.text()).toContain('Connect GitHub')
    expect(wrapper.text()).toContain('Turn on notifications')
    expect(wrapper.text()).toContain('Tokens are stored in your OS keychain.')
  })

  // Hive setup goes first — it is the one answer the rest of the app reads
  // back — then the profile, which needs no credential, then connecting.
  it('orders the steps and marks the ones already past as done', () => {
    const profile = mountScreen({ card: 'profile' }).findAll('ol li')
    expect(profile[0].text()).toContain('Set up your agent and code')
    expect(profile[1].text()).toContain('Create your first profile')
    expect(profile[2].text()).toContain('Connect GitHub')
    // The active step shows its number; steps before it show a check icon.
    expect(profile[0].text()).toContain('complete')
    expect(profile[1].text()).toContain('2')
    expect(profile[1].attributes('aria-current')).toBe('step')

    const connecting = mountScreen({ card: 'idle' }).findAll('ol li')
    expect(connecting[1].text()).not.toContain('2')
    expect(connecting[1].attributes('aria-current')).toBeUndefined()
    expect(connecting[1].text()).toContain('complete')
    expect(connecting[2].text()).toContain('3')
    expect(connecting[2].attributes('aria-current')).toBe('step')
  })

  // The screen stays mounted across the step switch, so a mount-time
  // autofocus would have fired while the Hive card was on screen and found no
  // input at all.
  it('focuses the profile input whether it mounts there or arrives from the hive step', async () => {
    const direct = mount(OnboardingScreen, {
      attachTo: document.body,
      props: { card: 'profile', deviceFlow: null, error: null, busy: false, githubConnected: false },
    })
    await flushPromises()
    expect(document.activeElement).toBe(direct.get('[data-testid="onboarding-profile-input"]').element)
    direct.unmount()

    const hive = await loadedHive()
    const walked = mount(OnboardingScreen, {
      attachTo: document.body,
      props: { card: 'hive', hive, deviceFlow: null, error: null, busy: false, githubConnected: false },
    })
    await flushPromises()
    await walked.setProps({ card: 'profile' })
    await flushPromises()
    expect(document.activeElement).toBe(walked.get('[data-testid="onboarding-profile-input"]').element)
    walked.unmount()
  })

  it('asks for a profile name and emits the trimmed value', async () => {
    const wrapper = mountScreen({ card: 'profile' })
    expect(wrapper.text()).toContain('Profiles separate feeds, sources, and rules.')
    expect(wrapper.text()).toContain('Work, Open Source, or Personal')
    expect(wrapper.text()).toContain('Create profile')
    expect(wrapper.text()).not.toContain('workspace')

    const input = wrapper.get('[data-testid="onboarding-profile-input"]')
    expect(wrapper.get('label[for="onboarding-profile-name"]').text()).toBe('Profile name')
    expect(input.attributes('placeholder')).toBe('Personal')
    expect(input.attributes('aria-describedby')).toBe('onboarding-profile-description')
    await input.setValue('  Frontend Triage  ')
    await wrapper.get('[data-testid="onboarding-profile-submit"]').trigger('click')
    expect(wrapper.emitted('createProfile')).toEqual([['Frontend Triage']])
  })

  it('omits the already-completed connection step for a connected account', () => {
    const wrapper = mountScreen({ card: 'profile', githubConnected: true })
    expect(wrapper.findAll('ol li')).toHaveLength(1)
    expect(wrapper.text()).toContain('Name a profile to organize its feeds, sources, and rules.')
    expect(wrapper.text()).not.toContain('Connect GitHub')
  })

  it('emits startDeviceFlow from the idle card', async () => {
    const wrapper = mountScreen()
    await wrapper.get('[data-testid="onboarding-connect"]').trigger('click')
    expect(wrapper.emitted('startDeviceFlow')).toHaveLength(1)
  })

  it('shows the user code and waiting state on the device card', () => {
    const wrapper = mountScreen({
      card: 'device',
      deviceFlow: { userCode: '7B4C-Q22F', verificationUri: 'https://github.com/login/device' },
    })
    expect(wrapper.get('[data-testid="onboarding-user-code"]').text()).toBe('7B4C-Q22F')
    expect(wrapper.text()).toContain('Waiting for authorization…')
    expect(wrapper.get('[data-testid="onboarding-open-verification"]').text()).toContain('github.com/login/device')
  })

  it('submits a trimmed token and disables submit when empty', async () => {
    const wrapper = mountScreen({ card: 'token' })
    const submit = wrapper.get('[data-testid="onboarding-token-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="onboarding-token-input"]').setValue('  ghp_abc  ')
    await submit.trigger('click')
    expect(wrapper.emitted('submitToken')).toEqual([['ghp_abc']])
  })

  it('switches cards via the secondary links', async () => {
    const wrapper = mountScreen()
    await wrapper.get('[data-testid="onboarding-use-token"]').trigger('click')
    expect(wrapper.emitted('useTokenInstead')).toHaveLength(1)

    const tokenCard = mountScreen({ card: 'token' })
    await tokenCard.get('[data-testid="onboarding-back"]').trigger('click')
    expect(tokenCard.emitted('backToStart')).toHaveLength(1)
  })

  it('renders errors on the active card', () => {
    const wrapper = mountScreen({ error: 'Could not reach GitHub to start sign-in.' })
    expect(wrapper.get('[data-testid="onboarding-error"]').text()).toContain('Could not reach GitHub')
  })

  it('ignores Enter on the profile card while busy', async () => {
    const wrapper = mountScreen({ card: 'profile' })
    await wrapper.get('[data-testid="onboarding-profile-input"]').setValue('Frontend Triage')
    await wrapper.setProps({ busy: true })
    const input = wrapper.get('[data-testid="onboarding-profile-input"]')
    expect(input.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="onboarding-profile-submit"]').text()).toContain('Creating')
    await input.trigger('keydown.enter')
    expect(wrapper.emitted('createProfile')).toBeUndefined()
  })

  it('ignores Enter on the token card while busy', async () => {
    const wrapper = mountScreen({ card: 'token', busy: true })
    await wrapper.get('[data-testid="onboarding-token-input"]').setValue('ghp_abc')
    await wrapper.get('[data-testid="onboarding-token-input"]').trigger('keydown.enter')
    expect(wrapper.emitted('submitToken')).toBeUndefined()
  })

  // Bypassing is possible, but only past the warning — a feed with no sources
  // is not something to discover later.
  it('warns before skipping the connect step, and can back out of the warning', async () => {
    const wrapper = mountScreen({ card: 'idle' })

    expect(wrapper.get('[data-testid="onboarding-skip"]').text()).toBe('Continue without GitHub')
    await wrapper.get('[data-testid="onboarding-skip"]').trigger('click')
    expect(wrapper.text()).toContain('Skip connecting GitHub?')
    expect(wrapper.text()).toContain('Settings ▸ Integrations')
    expect(wrapper.find('[data-testid="onboarding-connect"]').exists()).toBe(false)
    expect(wrapper.emitted('skipConnect')).toBeUndefined()

    await wrapper.get('[data-testid="onboarding-skip-back"]').trigger('click')
    expect(wrapper.get('[data-testid="onboarding-connect"]').isVisible()).toBe(true)

    await wrapper.get('[data-testid="onboarding-skip"]').trigger('click')
    await wrapper.get('[data-testid="onboarding-skip-confirm"]').trigger('click')
    expect(wrapper.emitted('skipConnect')).toHaveLength(1)
  })

  it('offers the skip on every connect card but never on the profile step', () => {
    for (const card of ['idle', 'device', 'token'] as const) {
      expect(mountScreen({ card }).find('[data-testid="onboarding-skip"]').exists()).toBe(true)
    }
    // Nothing to skip past yet — the profile is what first run is creating.
    expect(mountScreen({ card: 'profile' }).find('[data-testid="onboarding-skip"]').exists()).toBe(false)
  })

  it('emits on Enter when not busy and text is present', async () => {
    const wrapper = mountScreen({ card: 'token', busy: false })
    await wrapper.get('[data-testid="onboarding-token-input"]').setValue('ghp_abc')
    await wrapper.get('[data-testid="onboarding-token-input"]').trigger('keydown.enter')
    expect(wrapper.emitted('submitToken')).toEqual([['ghp_abc']])
  })

  // The permission grant is the last step: it marks the notifications step
  // active and asks for the OS grant rather than leaving it to a mid-usage
  // dialog.
  it('marks the notifications step active on the permissions card', () => {
    const steps = mountScreen({ card: 'permissions', permission: 'not-requested' }).findAll('ol li').map((li) => li.text())
    expect(steps[0]).not.toContain('1')
    expect(steps[1]).not.toContain('2')
    expect(steps[2]).not.toContain('3')
    expect(steps[3]).toContain('Turn on notifications')
    expect(steps[3]).toContain('4')
  })

  it('requests permission from the not-requested permissions card, and offers an honest skip', async () => {
    const wrapper = mountScreen({ card: 'permissions', permission: 'not-requested' })
    // The GitHub skip belongs to the connect cards; this card has its own.
    expect(wrapper.find('[data-testid="onboarding-skip"]').exists()).toBe(false)

    await wrapper.get('[data-testid="onboarding-permissions-allow"]').trigger('click')
    expect(wrapper.emitted('requestPermission')).toHaveLength(1)

    expect(wrapper.text()).toContain('Activity')
    expect(wrapper.get('[data-testid="onboarding-permissions-skip"]').text()).toBe('Not now')
    await wrapper.get('[data-testid="onboarding-permissions-skip"]').trigger('click')
    expect(wrapper.emitted('finishPermissions')).toHaveLength(1)
  })

  it('disables the allow button and shows progress while requesting', () => {
    const wrapper = mountScreen({ card: 'permissions', permission: 'not-requested', busy: true })
    const allow = wrapper.get('[data-testid="onboarding-permissions-allow"]')
    expect(allow.attributes('disabled')).toBeDefined()
    expect(allow.text()).toContain('Requesting')
  })

  it('confirms the grant and finishes from the granted permissions card', async () => {
    const wrapper = mountScreen({ card: 'permissions', permission: 'granted' })
    expect(wrapper.find('[data-testid="onboarding-permissions-allow"]').exists()).toBe(false)
    await wrapper.get('[data-testid="onboarding-permissions-finish"]').trigger('click')
    expect(wrapper.emitted('finishPermissions')).toHaveLength(1)
  })

  it('shows guidance instead of a dead button when permission is denied', async () => {
    const wrapper = mountScreen({ card: 'permissions', permission: 'denied' })
    expect(wrapper.find('[data-testid="onboarding-permissions-allow"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="onboarding-permissions-denied-guidance"]').text()).toContain('blocked')
    await wrapper.get('[data-testid="onboarding-permissions-finish"]').trigger('click')
    expect(wrapper.emitted('finishPermissions')).toHaveLength(1)
  })
})

// The Hive step is first run's answer to "which agent, and where is my code" —
// the two values the session launcher is built from. Without them the new
// session picker has nothing to offer, which is the state this step exists to
// prevent.
describe('OnboardingScreen — Hive setup', () => {
  it('starts on the agent this machine actually has', async () => {
    const hive = await loadedHive()
    const wrapper = mountScreen({ card: 'hive', hive })

    expect(wrapper.get('[data-testid="hive-agent-opencode"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="hive-agent-claude"]').attributes('aria-pressed')).toBe('false')
    expect(hive.defaultAgent.value).toBe('opencode')
  })

  it('cannot be submitted until a folder is chosen', async () => {
    const hive = await loadedHive()
    hiveMocks.ChooseDirectory.mockResolvedValue('/home/u/code')
    hiveMocks.InspectWorkspace.mockResolvedValue({ path: '/home/u/code', exists: true, repos: 7 })
    const wrapper = mountScreen({ card: 'hive', hive })

    expect(wrapper.get('[data-testid="onboarding-hive-submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="hive-add-workspace"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-workspace-list"]').text()).toContain('7 repositories')
    expect(wrapper.get('[data-testid="onboarding-hive-submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="onboarding-hive-submit"]').trigger('click')
    expect(wrapper.emitted('saveHive')).toHaveLength(1)
  })

  // Skipping is a real option: the inbox half of the app works without any of
  // this, and a first run that cannot be got past is worse than an empty
  // session picker.
  it('offers a skip that says what skipping costs', async () => {
    const hive = await loadedHive()
    const wrapper = mountScreen({ card: 'hive', hive })

    expect(wrapper.text()).toContain('the inbox works without this')
    await wrapper.get('[data-testid="onboarding-hive-skip"]').trigger('click')
    expect(wrapper.emitted('skipHive')).toHaveLength(1)
  })

  it('shows an existing config to confirm instead of a form to fill in', async () => {
    const hive = await loadedHive(hiveSetup({
      exists: true,
      usable: true,
      defaultAgent: 'opencode',
      profiles: [{ name: 'opencode', command: 'opencode', flags: [] }],
      workspaces: [{ path: '/home/u/code', exists: true, repos: 9 }],
    }))
    const wrapper = mountScreen({ card: 'hive', hive, hiveConfigured: true })

    const existing = wrapper.get('[data-testid="onboarding-hive-existing"]').text()
    expect(existing).toContain('/home/u/.config/hive/config.yaml')
    expect(existing).toContain('opencode')
    expect(existing).toContain('1 folder')
    expect(wrapper.find('[data-testid="hive-add-workspace"]').exists()).toBe(false)

    await wrapper.get('[data-testid="onboarding-hive-continue"]').trigger('click')
    expect(wrapper.emitted('skipHive')).toHaveLength(1)
  })

  it('surfaces a save failure on the step rather than moving on', async () => {
    const hive = await loadedHive()
    hiveMocks.ChooseDirectory.mockResolvedValue('/home/u/code')
    hiveMocks.InspectWorkspace.mockResolvedValue({ path: '/home/u/code', exists: true, repos: 2 })
    hiveMocks.Save.mockRejectedValue(new Error('saving the Hive configuration: permission denied'))
    const wrapper = mountScreen({ card: 'hive', hive })

    await wrapper.get('[data-testid="hive-add-workspace"]').trigger('click')
    await flushPromises()
    expect(await hive.save()).toBe(false)
    await flushPromises()

    expect(wrapper.get('[data-testid="onboarding-hive-error"]').text()).toContain('permission denied')
  })
})
