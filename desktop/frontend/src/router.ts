import { defineComponent } from 'vue'
import {
  createRouter,
  createWebHashHistory,
  type RouteRecordRaw,
  type Router,
  type RouterHistory,
} from 'vue-router'

export type AppRouteName = 'feed' | 'flows' | 'activity' | 'terminal' | 'application-settings' | 'profile-settings' | 'dev'

// The one list of application settings sections. It builds the route's own
// section matcher below and backs isApplicationSettingsSection, which App.vue
// uses to resolve :section — a second hand-written list is how a section ends
// up routable but unreachable, silently falling through to the default pane.
// Order is presentation order in SettingsView's nav.
export const applicationSettingsSections = [
  'appearance',
  'keybindings',
  'integrations',
  'actions',
  'skills',
  'system',
  'notifications',
] as const
export type ApplicationSettingsSection = (typeof applicationSettingsSections)[number]

export function isApplicationSettingsSection(value: unknown): value is ApplicationSettingsSection {
  return typeof value === 'string' && (applicationSettingsSections as readonly string[]).includes(value)
}

export const profileSettingsSections = ['general', 'danger'] as const
export type ProfileSettingsSection = (typeof profileSettingsSections)[number]

export function isProfileSettingsSection(value: unknown): value is ProfileSettingsSection {
  return typeof value === 'string' && (profileSettingsSections as readonly string[]).includes(value)
}

// App.vue owns the persistent desktop shell and renders the matched page in
// its main slot. Vue Router still requires a component on leaf route records.
const ShellPage = defineComponent({ name: 'ShellPage', render: () => null })

export function createAppRouter(history: RouterHistory = createWebHashHistory()): Router {
  const routes: RouteRecordRaw[] = [
    { path: '/', redirect: { name: 'feed' } },
    {
      path: '/feed/:profileId?',
      name: 'feed',
      component: ShellPage,
    },
    {
      path: '/flows/:profileId',
      name: 'flows',
      component: ShellPage,
    },
    {
      // Activity is app-global (the audit log spans every profile), so it
      // takes no profileId param.
      path: '/activity',
      name: 'activity',
      component: ShellPage,
    },
    {
      // Terminal mode is app-global too — sessions belong to repos, not
      // profiles. :slug is the attached tmux session; ?window pins its active
      // window. Both live in the URL so history traversal and mode re-entry
      // restore the exact surface the user left.
      path: '/terminal/:slug?',
      name: 'terminal',
      component: ShellPage,
    },
    {
      path: `/settings/:section(${applicationSettingsSections.join('|')})?`,
      name: 'application-settings',
      component: ShellPage,
    },
    {
      path: `/profiles/:profileId/settings/:section(${profileSettingsSections.join('|')})?`,
      name: 'profile-settings',
      component: ShellPage,
    },
  ]

  // Keep this record out of production bundles entirely; the catchall below
  // handles /dev there just like any other unknown route.
  if (import.meta.env.DEV) {
    routes.push({ path: '/dev', name: 'dev', component: ShellPage })
  }

  routes.push({ path: '/:pathMatch(.*)*', redirect: { name: 'feed' } })

  return createRouter({ history, routes })
}

export const router = createAppRouter()
