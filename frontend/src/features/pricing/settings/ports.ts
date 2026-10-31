import { inject, type InjectionKey } from 'vue'
import type { SiteBillingMode } from './contracts'
import type { BillingModeDraft } from './billingModeModel'
export type { SiteBillingMode }
export interface PricingSettingsServices {
  readonly modes: readonly { value: SiteBillingMode; labelKey: string; hintKey: string }[]
  resolve(settings: Readonly<BillingModeDraft>): SiteBillingMode
  patch(mode: SiteBillingMode): BillingModeDraft
}
export const pricingSettingsServicesKey: InjectionKey<PricingSettingsServices> = Symbol('pricing.settings')
export function usePricingSettingsServices(): PricingSettingsServices {
  const value = inject(pricingSettingsServicesKey)
  if (!value) throw new Error('Pricing settings services are not provided')
  return value
}
