import type { RouteRecordRaw } from 'vue-router'
/** App selects the user shell for authenticated ?embedded=1; public remains anonymous. */
export const catalogRoutes: RouteRecordRaw[] = [{
    path: '/model-plaza',
    name: 'ModelPlaza',
    component: () => import('./pages/ModelPlazaPage.vue'),
    meta: { domain: 'catalog',  requiresAuth: false, title: 'Model Plaza', titleKey: 'modelPlaza.title', shell: 'public' }
  },
{
    path: '/available-channels',
    name: 'UserAvailableChannels',
    component: () => import('@/features/catalog/pages/AvailableChannelsPage.vue'),
    meta: { domain: 'catalog', navigation: {order: 50, icon: 'channel', labelKey: 'nav.availableChannels', hideInSimpleMode: true, feature: 'availableChannels'},  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Available Channels',
      titleKey: 'availableChannels.title',
      descriptionKey: 'availableChannels.description'
    }
  }
]
