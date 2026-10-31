import type { RouteRecordRaw } from 'vue-router'
export const pricingRoutes: RouteRecordRaw[] = [
 { meta: { domain: 'pricing', navigationGroup: {id: 'channels', labelKey: 'nav.channelManagement', icon: 'channel', order: 50, surface: 'admin', hideInSimpleMode: true} },  path: '/admin/channels', redirect: '/admin/channels/pricing' },
 {
    path: '/admin/channels/pricing',
    name: 'AdminChannels',
    component: () => import('./pages/ChannelsPage.vue'),
    meta: { domain: 'pricing', navigation: {order: 10, group: 'channels', icon: 'priceTag', labelKey: 'nav.channelPricing'},  requiresAuth: true, requiresAdmin: true, title: 'Channel Management', titleKey: 'admin.channels.title', descriptionKey: 'admin.channels.description', shell: 'admin' }
  }
]
