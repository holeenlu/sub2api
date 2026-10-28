import type { BILLING_MODE_TOKEN, BILLING_MODE_PER_REQUEST, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS } from '@/features/pricing'
import type { GroupBadge } from '@/features/access-policy/presentation'
import type { PeakRatePresentation } from '@/features/pricing'
import { inject, type InjectionKey, type Ref } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
export interface CatalogPresentation {
  readonly peakRate: PeakRatePresentation
  /** Pricing-owned display taxonomy, supplied by app without a runtime domain dependency. */
  readonly pricing: {
    readonly token: typeof BILLING_MODE_TOKEN
    readonly perRequest: typeof BILLING_MODE_PER_REQUEST
    readonly image: typeof BILLING_MODE_IMAGE
    readonly video: typeof BILLING_MODE_VIDEO
    readonly reasoningEffortLevels: typeof REASONING_EFFORT_LEVELS
  }
  readonly components: { GroupBadge: typeof GroupBadge }
  authenticated: boolean
  embedded: boolean
  dashboardTarget: RouteLocationRaw
  loginTarget: RouteLocationRaw
  settingsLoaded: boolean
  refreshSettings(): Promise<unknown>
  settings: { site_name?: string; site_logo?: string; server_utc_offset?: string; model_plaza_enabled?: boolean; model_plaza_require_auth?: boolean } | null
}
/** App includes session epoch, portal and business scope; anonymous has its own key. */
export const catalogScopeKey: InjectionKey<Readonly<Ref<string>>> = Symbol('catalog.scope')
export const catalogPresentationKey: InjectionKey<Readonly<Ref<CatalogPresentation>>> = Symbol('catalog.presentation')
export function requireCatalogPort<T>(key: InjectionKey<T>): T {
  const value = inject(key)
  if (value === undefined) throw new Error('Missing catalog app binding: ' + key.description)
  return value
}

export interface CatalogNotifications { showError(message: string): void }
export const catalogNotificationsKey: InjectionKey<CatalogNotifications> = Symbol('catalog.notifications')
/** App supplies the access-policy user-rate lookup for this catalog scope. */
export const catalogUserGroupRatesKey: InjectionKey<(signal: AbortSignal) => Promise<Record<number, number>>> = Symbol('catalog.user-group-rates')
