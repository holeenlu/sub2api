import type { RouteRecordRaw } from 'vue-router'
export const subscriptionsRoutes: RouteRecordRaw[] = [
  {
    path: '/subscriptions',
    name: 'Subscriptions',
    component: () => import('./pages/UserSubscriptionsPage.vue'),
    meta: { domain: 'subscriptions', navigation: {order: 70, icon: 'creditCard', labelKey: 'nav.mySubscriptions', hideInSimpleMode: true, feature: 'subscription'},  requiresAuth: true, requiresAdmin: false, title: 'My Subscriptions', titleKey: 'userSubscriptions.title', descriptionKey: 'userSubscriptions.description', requiresSubscription: true, shell: 'user' }
  },
  {
    path: '/admin/subscriptions',
    name: 'AdminSubscriptions',
    component: () => import('./pages/AdminSubscriptionsPage.vue'),
    meta: { domain: 'subscriptions', navigation: {order: 60, icon: 'creditCard', labelKey: 'nav.subscriptions', hideInSimpleMode: true, feature: 'subscription'},  requiresAuth: true, requiresAdmin: true, title: 'Subscription Management', titleKey: 'admin.subscriptions.title', descriptionKey: 'admin.subscriptions.description', shell: 'admin' }
  }
]
