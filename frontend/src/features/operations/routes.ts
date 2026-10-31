import type { RouteRecordRaw } from 'vue-router'
export const operationsRoutes: RouteRecordRaw[] = [
  {
    path: '/admin/ops',
    name: 'AdminOps',
    component: () => import('./dashboard/OpsDashboard.vue'),
    meta: { domain: 'operations', navigation: {order: 20, icon: 'chart', labelKey: 'nav.ops', feature: 'opsMonitoring'},  requiresAuth: true, requiresAdmin: true, title: 'Ops Monitoring', titleKey: 'admin.ops.title', descriptionKey: 'admin.ops.description', shell: 'admin' }
  },
  {
    path: '/admin/audit-logs',
    name: 'AdminAuditLogs',
    component: () => import('./pages/AuditLogPage.vue'),
    meta: { domain: 'operations', navigation: {order: 140, icon: 'shield', labelKey: 'nav.auditLogs', hideInSimpleMode: true},  requiresAuth: true, requiresAdmin: true, title: 'Audit Logs', titleKey: 'admin.audit.title', descriptionKey: 'admin.audit.description', shell: 'admin' }
  },
  {
    path: '/admin/prompt-audit',
    name: 'AdminPromptAudit',
    component: () => import('./promptAudit/PromptAuditView.vue'),
    meta: { domain: 'operations', navigation: {order: 20, group: 'security-audit', icon: 'shield', labelKey: 'nav.promptAudit'},  requiresAuth: true, requiresAdmin: true, title: 'Prompt Audit', titleKey: 'admin.promptAudit.title', descriptionKey: 'admin.promptAudit.description', requiresRiskControl: true, shell: 'admin' }
  },
{
    path: '/admin/channels/monitor',
    name: 'AdminChannelMonitor',
    component: () => import('@/features/operations/monitor/pages/ChannelMonitorPage.vue'),
    meta: { domain: 'operations', navigation: {order: 20, group: 'channels', icon: 'signal', labelKey: 'nav.channelMonitor', feature: 'channelMonitor'},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Channel Monitor',
      titleKey: 'admin.channelMonitor.title',
      descriptionKey: 'admin.channelMonitor.description'
    }
  },
{
    path: '/monitor',
    name: 'ChannelStatus',
    component: () => import('@/features/operations/monitor/pages/ChannelStatusPage.vue'),
    meta: { domain: 'operations', navigation: {order: 60, icon: 'signal', feature: 'channelMonitor'},  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Channel Status',
      titleKey: 'nav.channelStatus'
    }
  }
]
