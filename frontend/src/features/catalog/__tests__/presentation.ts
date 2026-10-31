import { GroupBadge } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import { BILLING_MODE_TOKEN, BILLING_MODE_PER_REQUEST, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS } from '@/features/pricing'
import type { CatalogPresentation } from '../ports'

/** Mirrors the narrow app binding with the real pricing taxonomy. */
export function catalogPresentationFixture(overrides: Partial<CatalogPresentation> = {}): CatalogPresentation {
  return {
    peakRate: peakRatePresentation,
    components: { GroupBadge },
    pricing: {
      token: BILLING_MODE_TOKEN,
      perRequest: BILLING_MODE_PER_REQUEST,
      image: BILLING_MODE_IMAGE,
      video: BILLING_MODE_VIDEO,
      reasoningEffortLevels: REASONING_EFFORT_LEVELS
    },
    authenticated: false,
    embedded: false,
    dashboardTarget: '/dashboard',
    loginTarget: '/login',
    settingsLoaded: true,
    refreshSettings: async () => undefined,
    settings: { model_plaza_enabled: true },
    ...overrides
  }
}
