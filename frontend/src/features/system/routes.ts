import type { RouteRecordRaw } from 'vue-router'
export const systemRoutes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('./pages/SetupWizardPage.vue'),
    meta: { domain: 'system',  requiresAuth: false, title: 'Setup', titleKey: 'setup.pageTitle' }
  },
  {
    path: '/admin/plugins',
    name: 'AdminPlugins',
    component: () => import('./pages/PluginsPage.vue'),
    meta: { domain: 'system', navigation: {order: 80, icon: 'plugin', labelKey: 'nav.plugins', feature: 'pluginManagement'},  requiresAuth: true, requiresAdmin: true, title: 'Plugin Management', titleKey: 'admin.plugins.title', descriptionKey: 'admin.plugins.description', shell: 'admin' }
  }
]
