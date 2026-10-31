import type { RouteRecordRaw } from 'vue-router'
export const usageRoutes: RouteRecordRaw[] = [{
    path: '/usage',
    name: 'Usage',
    component: () => import('./pages/UserUsagePage.vue'),
    meta: { domain: 'usage', navigation: {order: 40, icon: 'chart', labelKey: 'nav.usage', hideInSimpleMode: true},  requiresAuth: true, requiresAdmin: false, title: 'Usage Records', titleKey: 'usage.title', descriptionKey: 'usage.description', shell: 'user' }
  },
{
    path: '/admin/usage',
    name: 'AdminUsage',
    component: () => import('./pages/AdminUsagePage.vue'),
    meta: { domain: 'usage', navigation: {order: 130, icon: 'chart', labelKey: 'nav.usage'},  requiresAuth: true, requiresAdmin: true, title: 'Usage Records', titleKey: 'admin.usage.title', descriptionKey: 'admin.usage.description', shell: 'admin' }
  },
{
    path: '/key-usage',
    name: 'KeyUsage',
    component: () => import('@/features/usage/pages/PublicKeyUsagePage.vue'),
    meta: { domain: 'usage', 
      requiresAuth: false,
      title: 'Key Usage',
      titleKey: 'keyUsage.title',
    }
  }
]
