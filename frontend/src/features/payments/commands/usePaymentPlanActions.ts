import { computed, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import { adminPaymentAPI } from '../api/adminPayment'
import { usePaymentsServices } from '../services'
import { usePaymentPlansServices } from '../planServices'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function usePaymentPlanActions(plans: Ref<SubscriptionPlan[]>, reload: () => Promise<void>) {
  const services = usePaymentsServices(), { permissions } = usePaymentPlansServices(), { t } = useI18n()
  const context = computed(() => services.scopeKey.value === null ? null : services.scopeKey.value + ':' + JSON.stringify(permissions.value))
  const request = usePaymentRequest(() => context.value)
  const showPlanDialog = ref(false), showDeletePlanDialog = ref(false), editingPlan = ref<SubscriptionPlan | null>(null), deletingPlanId = ref<number | null>(null)
  const pendingId = ref<number | null>(null), uncertainIds = ref(new Set<number>())
  const canCreate = computed(() => permissions.value.canManagePlans && permissions.value.canUpdateRates)
  const editableFields = computed(() => editingPlan.value ? permissions.value.updateFields : permissions.value.createFields)
  const canToggleForSale = computed(() => permissions.value.canManagePlans && (permissions.value.updateFields === undefined || permissions.value.updateFields.includes('for_sale')))
  function openPlanEdit(plan: SubscriptionPlan | null): void {
    if (!permissions.value.canManagePlans || (!plan && !canCreate.value)) return
    editingPlan.value = plan; showPlanDialog.value = true
  }
  function confirmDeletePlan(plan: SubscriptionPlan): void {
    if (!permissions.value.canManagePlans || pendingId.value !== null || uncertainIds.value.has(plan.id)) return
    deletingPlanId.value = plan.id; showDeletePlanDialog.value = true
  }
  async function run(id: number, nextForSale?: boolean): Promise<void> {
    if (!permissions.value.canManagePlans || pendingId.value !== null || uncertainIds.value.has(id)) return
    if (nextForSale !== undefined && !canToggleForSale.value) return
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    pendingId.value = id
    try {
      if (nextForSale === undefined) await adminPaymentAPI.deletePlan(id, ticket)
      else await adminPaymentAPI.updatePlan(id, { for_sale: nextForSale }, ticket)
      if (!ticket.isCurrent()) return
      if (nextForSale === undefined) { services.notifySuccess(t('common.deleted')); showDeletePlanDialog.value = false; await reload() }
      else plans.value = plans.value.map(plan => plan.id === id ? { ...plan, for_sale: nextForSale } : plan)
    } catch (error) {
      if (!ticket.isCurrent()) return
      if (paymentSubmissionMayBeUnknown(error)) uncertainIds.value = new Set(uncertainIds.value).add(id)
      services.notifyError(extractPaymentErrorMessage(error, t, t('common.error')))
    } finally { if (ticket.isCurrent()) pendingId.value = null }
  }
  watch(context, () => { pendingId.value = null; showPlanDialog.value = false; showDeletePlanDialog.value = false; editingPlan.value = null; deletingPlanId.value = null }, { flush: 'sync' })
  watch(services.scopeKey, () => { uncertainIds.value = new Set() }, { flush: 'sync' })
  return { showPlanDialog, showDeletePlanDialog, editingPlan, canCreate, editableFields, canToggleForSale, pendingId, uncertainIds, openPlanEdit, confirmDeletePlan,
    toggleForSale: (plan: SubscriptionPlan) => run(plan.id, !plan.for_sale), handleDeletePlan: () => deletingPlanId.value === null ? Promise.resolve() : run(deletingPlanId.value) }
}
