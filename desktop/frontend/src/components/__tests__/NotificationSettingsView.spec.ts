import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import NotificationSettingsView from '../NotificationSettingsView.vue'
import { resetNotificationSettingsForTests } from '../../composables/useNotificationSettings'

const mocks = vi.hoisted(() => ({
  NotificationSettings: vi.fn(),
  SetNotificationSettings: vi.fn(),
  PermissionStatus: vi.fn(),
  RequestNotificationPermission: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  NotificationSettings: mocks.NotificationSettings,
  SetNotificationSettings: mocks.SetNotificationSettings,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/notificationservice', () => ({
  PermissionStatus: mocks.PermissionStatus,
  RequestNotificationPermission: mocks.RequestNotificationPermission,
}))

beforeEach(() => {
  vi.clearAllMocks()
  resetNotificationSettingsForTests()
  mocks.NotificationSettings.mockResolvedValue({
    notificationsEnabled: true,
    delivery: 'auto',
    notificationSound: true,
  })
  mocks.SetNotificationSettings.mockResolvedValue(undefined)
  mocks.PermissionStatus.mockResolvedValue('not-requested')
  mocks.RequestNotificationPermission.mockResolvedValue(true)
})

describe('NotificationSettingsView', () => {
  it('renders preferences, disables delivery with master off, and persists the switch', async () => {
    const wrapper = mount(NotificationSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="notification-enable"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="notification-delivery-auto"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="notification-sound"]').exists()).toBe(true)

    await wrapper.find('[data-testid="notification-enable"]').trigger('click')
    await flushPromises()

    expect(mocks.SetNotificationSettings).toHaveBeenCalledWith({
      notificationsEnabled: false,
      delivery: 'auto',
      notificationSound: true,
    })
    expect(wrapper.find('[data-testid="notification-delivery-auto"]').attributes('disabled')).toBeDefined()
  })

  it('reflects the persisted delivery mode and persists a change', async () => {
    mocks.NotificationSettings.mockResolvedValue({
      notificationsEnabled: true,
      delivery: 'system',
      notificationSound: true,
    })
    const wrapper = mount(NotificationSettingsView)
    await flushPromises()

    expect(wrapper.find<HTMLInputElement>('[data-testid="notification-delivery-system"]').element.checked).toBe(true)

    await wrapper.find('[data-testid="notification-delivery-app"]').trigger('change')
    await flushPromises()

    expect(mocks.SetNotificationSettings).toHaveBeenCalledWith({
      notificationsEnabled: true,
      delivery: 'app',
      notificationSound: true,
    })
  })

  it('requests permission and renders denied guidance from the live state', async () => {
    const wrapper = mount(NotificationSettingsView)
    await flushPromises()
    expect(wrapper.find('[data-testid="notification-permission-status"]').text()).toBe('Not requested')

    mocks.RequestNotificationPermission.mockResolvedValue(false)
    mocks.PermissionStatus.mockResolvedValue('denied')
    await wrapper.find('[data-testid="notification-permission-request"]').trigger('click')
    await flushPromises()

    expect(mocks.RequestNotificationPermission).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="notification-permission-status"]').text()).toBe('Denied')
    expect(wrapper.find('[data-testid="notification-permission-denied-guidance"]').exists()).toBe(true)
  })
})
