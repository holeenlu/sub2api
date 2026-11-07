import { peakRatePresentation } from '@/features/pricing'
import { ref } from 'vue'
import { config } from '@vue/test-utils'
import { GroupBadge, groupPresentationKey } from '@/features/access-policy/presentation'
import { vi } from 'vitest'
import type { PaymentPlansServices } from '../planServices'
export function createPaymentPlansServices(overrides: Partial<PaymentPlansServices> = {}): PaymentPlansServices {
  config.global.provide[groupPresentationKey as symbol] ??= { peakRate: peakRatePresentation, serverUtcOffset: ref(undefined), isSimpleMode: ref(false), platformLabel: (platform: string) => platform }
  return { components: { GroupBadge }, permissions: ref({ canManagePlans: true, canUpdateRates: true, canReadPaymentConfig: true }), listGroups: vi.fn().mockResolvedValue([]), ...overrides }
}
