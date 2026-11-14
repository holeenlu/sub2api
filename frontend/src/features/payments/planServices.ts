import type { GroupBadge } from '@/features/access-policy/presentation'
import { inject, type InjectionKey, type Ref } from 'vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
export type PaymentPlanGroup = Pick<AdminGroup, 'id' | 'name' | 'platform' | 'rate_multiplier' | 'subscription_type' | 'daily_limit_usd' | 'weekly_limit_usd' | 'monthly_limit_usd'>
export interface PaymentPlanPermissions {
  canManagePlans: boolean
  canUpdateRates: boolean
  canReadPaymentConfig: boolean
  /** Undefined means unrestricted; [] means no writable fields, matching admin_write_fields. */
  createFields?: readonly string[]
  updateFields?: readonly string[]
}
export interface PaymentPlansServices {
  readonly components: { GroupBadge: typeof GroupBadge }
  permissions: Readonly<Ref<PaymentPlanPermissions>>
  listGroups(options?: { signal?: AbortSignal }): Promise<PaymentPlanGroup[]>
}
export const paymentPlansServicesKey: InjectionKey<PaymentPlansServices> = Symbol('PaymentPlansServices')
export function usePaymentPlansServices(): PaymentPlansServices {
  const services = inject(paymentPlansServicesKey)
  if (!services) throw new Error('PaymentPlansServices must be provided by the application')
  return services
}
