import { defineAsyncComponent } from 'vue'
export const PaymentSettingsPanel = defineAsyncComponent(() => import('./settings/PaymentSettingsPanel.vue'))
export { projectPayment, paymentFields } from './settings/paymentModel'
export type { PaymentDraft } from './settings/paymentModel'
export const RechargeBonusSettingsPanel = defineAsyncComponent(() => import('./settings/RechargeBonusSettingsPanel.vue'))
export { projectRechargeBonus, rechargeBonusFields } from './settings/rechargeBonusModel'
export type { RechargeBonusDraft } from './settings/rechargeBonusModel'
export const ProvidersSettingsPanel = defineAsyncComponent(() => import('./settings/ProvidersSettingsPanel.vue'))
export { projectProviders, providersFields } from './settings/providersModel'
export type { ProvidersDraft } from './settings/providersModel'
export const BalanceNotificationSettingsPanel = defineAsyncComponent(() => import('./settings/BalanceNotificationSettingsPanel.vue'))
export { projectBalanceNotification, balanceNotificationFields } from './settings/balanceNotificationModel'
export type { BalanceNotificationDraft } from './settings/balanceNotificationModel'
export { paymentsSettingsServicesKey } from './settings/ports'
export type { PaymentsSettingsServices } from './settings/ports'
export { preparePaymentSettings } from './settings/preparePatch'
export type { RechargeBonusTierDraft, RechargeBonusMode } from './model/rechargeBonus'
export { getPaymentVisibleMethodSourceOptions, normalizePaymentVisibleMethodSource } from './settings/visibleMethods'
