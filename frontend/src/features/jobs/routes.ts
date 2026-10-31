import type { RouteRecordRaw } from 'vue-router'
export const jobsRoutes: RouteRecordRaw[] = [{
    path: '/batch-image',
    name: 'BatchImageGuide',
    alias: '/docs/batch-image',
    component: () => import('./pages/BatchImageJobsPage.vue'),
    meta: { domain: 'jobs', navigation: {order: 30, icon: 'batchImage', labelKey: 'nav.batchImage', hideInSimpleMode: true, feature: 'batchImageAccess'},  requiresAuth: true, requiresAdmin: false, title: 'Batch Image Guide', titleKey: 'batchImageGuide.title', descriptionKey: 'batchImageGuide.description', shell: 'user' }
  }]
