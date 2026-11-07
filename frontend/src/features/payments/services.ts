import { inject, type InjectionKey, type Ref } from 'vue'
import type { PeakRatePresentation } from '@/features/pricing'
import type { User } from '@/features/identity/api/profileTypes';
import type { UserSubscription } from '@/features/subscriptions/api/types';
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';
import type { PublicOrderVerifyResult } from './api/payment'

export type PublicOrderLookup = { resumeToken: string; outTradeNo?: never } | { outTradeNo: string; resumeToken?: never }
export interface PaymentsServices {
  readonly peakRate: PeakRatePresentation
  platformLabel(platform: string): string
  scopeKey: Readonly<Ref<string | null>>
  permissions: Readonly<Ref<{ canManageOrders: boolean; canRefundOrders: boolean }>>
  user: Readonly<Ref<Pick<User, 'id' | 'username' | 'balance'> | null>>
  publicSettings: Readonly<Ref<Pick<PublicSettings, 'server_utc_offset'> | null>>
  subscriptionEnabled: Readonly<Ref<boolean>>
  activeSubscriptions: Readonly<Ref<UserSubscription[]>>
  refreshUser(): Promise<unknown>
  refreshSubscriptions(force?: boolean): Promise<unknown>
  notifySuccess(message: string): void
  notifyError(message: string): void
  notifyInfo(message: string): void
  notifyWarning(message: string): void
  /** App uses the shared transport's explicit anonymous policy, never user order cache/JWT recovery. */
  resolvePublicOrder(lookup: PublicOrderLookup, options: { signal: AbortSignal }): Promise<PublicOrderVerifyResult>
}
export const paymentsServicesKey: InjectionKey<PaymentsServices> = Symbol('PaymentsServices')
export function usePaymentsServices(): PaymentsServices {
  const services = inject(paymentsServicesKey)
  if (!services) throw new Error('PaymentsServices must be provided by the application')
  return services
}
