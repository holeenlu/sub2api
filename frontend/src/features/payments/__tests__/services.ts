import { platformLabel } from '@/features/catalog'
import { peakRatePresentation } from '@/features/pricing'
import { ref } from 'vue'
import { vi } from 'vitest'
import type { PaymentsServices } from '../services'
export function createPaymentsServices(overrides: Partial<PaymentsServices> = {}): PaymentsServices {
  return {
    peakRate: peakRatePresentation,
    platformLabel,
    scopeKey: ref('session:9'), permissions: ref({ canManageOrders: false, canRefundOrders: false }), user: ref({ id: 9, username: 'demo-user', balance: 0 }),
    publicSettings: ref(null), subscriptionEnabled: ref(true), activeSubscriptions: ref([]),
    refreshUser: vi.fn().mockResolvedValue(undefined), refreshSubscriptions: vi.fn().mockResolvedValue(undefined),
    notifySuccess: vi.fn(), notifyError: vi.fn(), notifyInfo: vi.fn(), notifyWarning: vi.fn(),
    resolvePublicOrder: vi.fn().mockRejectedValue(new Error('No public fixture configured')),
    ...overrides,
  }
}
