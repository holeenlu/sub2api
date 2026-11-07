import type { RouteRecordRaw } from 'vue-router'
export const accessPolicyRoutes: RouteRecordRaw[] = [
{
    path: '/admin/risk-control',
    name: 'AdminRiskControl',
    component: () => import('@/features/access-policy/components/RiskControlPage.vue'),
    meta: { domain: 'access-policy', navigation: {order: 10, group: 'security-audit', icon: 'shield', labelKey: 'nav.contentModeration'}, navigationGroup: {id: 'security-audit', labelKey: 'nav.securityAudit', icon: 'shield', order: 110, surface: 'admin', feature: 'riskControl'},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Risk Control',
      titleKey: 'admin.riskControl.title',
      descriptionKey: 'admin.riskControl.description',
      requiresRiskControl: true
    }
  }
]
