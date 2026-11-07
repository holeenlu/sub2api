import type { RouteRecordRaw } from 'vue-router'

// The application supplies its user shell and existing authentication guards.
export const keysRoutes: RouteRecordRaw[] = [{
    path: '/keys',
    name: 'Keys',
    component: () => import('./pages/KeysPage.vue'),
    meta: { domain: 'keys', navigation: {order: 20, icon: 'key', labelKey: 'nav.apiKeys', adminSimpleOrder: 150, tour: 'sidebar-my-keys'},  requiresAuth: true, requiresAdmin: false, title: 'API Keys', titleKey: 'keys.title', descriptionKey: 'keys.description', shell: 'user' }
  }]
