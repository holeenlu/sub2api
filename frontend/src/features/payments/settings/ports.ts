import { inject, type InjectionKey, type Ref } from 'vue'



import type { adminPaymentAPI } from '../api/adminPayment'

export interface PaymentsSettingsServices {
  scopeKey: Readonly<Ref<string | null>>
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown; showWarning(message: string): unknown }
  payment: Pick<typeof adminPaymentAPI, 'createProvider' | 'deleteProvider' | 'getProviders' | 'updateProvider'>
  readonly docUrl: string
}
export const paymentsSettingsServicesKey: InjectionKey<PaymentsSettingsServices> = Symbol('payments.settings')
export function usePaymentsSettingsServices(): PaymentsSettingsServices {
  const value = inject(paymentsSettingsServicesKey)
  if (!value) throw new Error('Payments settings services are not provided')
  return value
}
