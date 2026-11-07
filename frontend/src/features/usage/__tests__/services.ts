import { ref } from 'vue'
import { vi } from 'vitest'
import type { UsageServices } from '../services'
export function createUsageTestServices(overrides: Partial<UsageServices> = {}): UsageServices {
  return {
    scopeKey: ref<string | null>('session:one|portal:admin|scope:all'),
    permissions: ref({ isSuperAdmin: true, canExport: true, canReadBillingCost: true }),
    publicSettings: ref({ allow_user_view_error_requests: true }), subscriptionEnabled: ref(true),
    notifySuccess: vi.fn(), notifyError: vi.fn(), notifyInfo: vi.fn(), notifyWarning: vi.fn(),
    copyText: vi.fn().mockResolvedValue(true),
    listUserKeys: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 }),
    getUserGroups: vi.fn().mockResolvedValue([]), getAdminUser: vi.fn(),
    listAdminGroups: vi.fn().mockResolvedValue([]), searchAdminAccounts: vi.fn().mockResolvedValue([]),
    getAdminSnapshot: vi.fn().mockResolvedValue({ trend: [], groups: [] }),
    getAdminModelStats: vi.fn().mockResolvedValue({ models: [] }),
    getAdminUserBreakdown: vi.fn().mockResolvedValue({ users: [] }), getAdminKeyBreakdown: vi.fn().mockResolvedValue({ api_keys: [] }),
    getAdminErrors: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    panels: {
      balanceHistory: { name: 'UserBalanceHistoryModal', props: ['show', 'user', 'hideActions'], template: '<div />' },
      adminErrorsTable: { name: 'OpsErrorLogTable', props: ['rows', 'total', 'loading', 'page', 'pageSize', 'visibleColumnKeys'], template: '<div />' },
      adminErrorDetail: { name: 'OpsErrorDetailModal', props: ['show', 'errorId', 'errorType'], template: '<div />' }
    }, ...overrides
  }
}
