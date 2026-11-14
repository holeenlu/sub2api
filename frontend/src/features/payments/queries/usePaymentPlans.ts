import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import { adminPaymentAPI, type AdminPaymentConfig } from '../api/adminPayment'
import { usePaymentsServices } from '../services'
import { usePaymentPlansServices, type PaymentPlanGroup } from '../planServices'
import { parsePaymentPlanFeatures } from '../model/paymentPlanDraft'
import { usePaymentRequest } from './usePaymentRequest'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function usePaymentPlans() {
  const services = usePaymentsServices(), plansServices = usePaymentPlansServices(), { t } = useI18n()
  const context = computed(() => services.scopeKey.value === null ? null : services.scopeKey.value + ':' + JSON.stringify(plansServices.permissions.value))
  const list = usePaymentRequest(() => context.value), groupsRequest = usePaymentRequest(() => context.value), configRequest = usePaymentRequest(() => context.value)
  const plans = ref<SubscriptionPlan[]>([]), plansLoading = ref(false), groups = ref<PaymentPlanGroup[]>([]), paymentConfig = ref<AdminPaymentConfig | null>(null)
  async function loadPlans(): Promise<void> {
    const ticket = list.begin()
    if (!ticket.isCurrent()) return
    plansLoading.value = true
    try {
      const response = await adminPaymentAPI.getPlans(ticket)
      if (ticket.isCurrent()) plans.value = (response.data ?? []).map(plan => ({ ...plan, features: parsePaymentPlanFeatures(plan.features) }))
    } catch (error) { if (ticket.isCurrent()) services.notifyError(extractPaymentErrorMessage(error, t, t('common.error'))) }
    finally { if (ticket.isCurrent()) plansLoading.value = false }
  }
  async function loadGroups(): Promise<void> {
    const ticket = groupsRequest.begin()
    if (!ticket.isCurrent()) return
    try { const result = await plansServices.listGroups(ticket); if (ticket.isCurrent()) groups.value = result }
    catch { /* Missing directory data never grants eligibility. */ }
  }
  async function loadPaymentConfig(): Promise<void> {
    if (!plansServices.permissions.value.canReadPaymentConfig) { paymentConfig.value = null; return }
    const ticket = configRequest.begin()
    if (!ticket.isCurrent()) return
    try { const response = await adminPaymentAPI.getConfig(ticket); if (ticket.isCurrent()) paymentConfig.value = response.data }
    catch { /* Optional payment preview does not block plan management. */ }
  }
  function load(): void { void loadPlans(); void loadGroups(); void loadPaymentConfig() }
  watch(context, () => { plans.value = []; groups.value = []; paymentConfig.value = null; plansLoading.value = false; if (context.value !== null) load() }, { flush: 'sync' })
  return { plans, plansLoading, groups, paymentConfig, loadPlans, load }
}
