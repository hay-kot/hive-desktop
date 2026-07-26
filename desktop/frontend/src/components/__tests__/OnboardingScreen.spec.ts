import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OnboardingScreen from '../OnboardingScreen.vue'

vi.mock('@wailsio/runtime', () => ({
  Browser: {
    OpenURL: vi.fn().mockResolvedValue(undefined),
  },
  Clipboard: { SetText: vi.fn().mockResolvedValue(undefined) },
}))

function mountScreen(props: Partial<InstanceType<typeof OnboardingScreen>['$props']> = {}) {
  return mount(OnboardingScreen, {
    props: {
      card: 'idle',
      deviceFlow: null,
      error: null,
      busy: false,
      ...props,
    },
  })
}

describe('OnboardingScreen', () => {
  // Two steps, because there are two screens. A step the walk can never make
  // active reads as a step that got skipped.
  it('lists exactly the steps onboarding has', () => {
    const wrapper = mountScreen()
    expect(wrapper.findAll('ol li').map((li) => li.text())).toHaveLength(2)
    expect(wrapper.text()).toContain('Create your first workspace')
    expect(wrapper.text()).toContain('Connect GitHub')
    expect(wrapper.text()).toContain('Tokens are stored in your OS keychain.')
  })

  // The workspace is the one step that needs no credential, so it goes first
  // and the connect cards are step 2.
  it('orders the workspace step ahead of connecting, and marks it done once past', () => {
    const workspace = mountScreen({ card: 'workspace' }).findAll('ol li').map((li) => li.text())
    expect(workspace[0]).toContain('Create your first workspace')
    expect(workspace[1]).toContain('Connect GitHub')
    // The active step shows its number; steps before it show a check icon.
    expect(workspace[0]).toContain('1')

    const connecting = mountScreen({ card: 'idle' }).findAll('ol li').map((li) => li.text())
    expect(connecting[0]).not.toContain('1')
    expect(connecting[1]).toContain('2')
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

  it('ignores Enter on the workspace card while busy', async () => {
    const wrapper = mountScreen({ card: 'workspace', busy: true })
    await wrapper.get('[data-testid="onboarding-workspace-input"]').setValue('Frontend Triage')
    await wrapper.get('[data-testid="onboarding-workspace-input"]').trigger('keydown.enter')
    expect(wrapper.emitted('createWorkspace')).toBeUndefined()
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

  it('offers the skip on every connect card but never on the workspace step', () => {
    for (const card of ['idle', 'device', 'token'] as const) {
      expect(mountScreen({ card }).find('[data-testid="onboarding-skip"]').exists()).toBe(true)
    }
    // Nothing to skip past yet — the workspace is what first run is creating.
    expect(mountScreen({ card: 'workspace' }).find('[data-testid="onboarding-skip"]').exists()).toBe(false)
  })

  it('emits on Enter when not busy and text is present', async () => {
    const wrapper = mountScreen({ card: 'token', busy: false })
    await wrapper.get('[data-testid="onboarding-token-input"]').setValue('ghp_abc')
    await wrapper.get('[data-testid="onboarding-token-input"]').trigger('keydown.enter')
    expect(wrapper.emitted('submitToken')).toEqual([['ghp_abc']])
  })
})
