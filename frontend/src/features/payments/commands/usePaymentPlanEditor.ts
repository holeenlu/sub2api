import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import { adminPaymentAPI, type AdminPaymentConfig } from '../api/adminPayment'
import { usePaymentsServices } from '../services'
import { usePaymentPlansServices, type PaymentPlanGroup } from '../planServices'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { emptyPaymentPlanDraft, paymentPlanDraft, buildPaymentPlanPayload, paymentPlanCnyPreview, parsePaymentPlanFeatures } from '../model/paymentPlanDraft'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
interface PlanEditorProps {
  show: boolean; plan: SubscriptionPlan | null; groups: PaymentPlanGroup[];
  editableFields?: readonly string[]; paymentConfig?: AdminPaymentConfig | null
}
export function usePaymentPlanEditor(props: Readonly<PlanEditorProps>, onSaved: () => void) {
  const services = usePaymentsServices(), { permissions } = usePaymentPlansServices(), { t } = useI18n()
  const context = computed(() => services.scopeKey.value === null || !props.show ? null
    : services.scopeKey.value + ':' + (props.plan?.id ?? 'new') + ':' + JSON.stringify([permissions.value, props.editableFields]))
  const request = usePaymentRequest(() => context.value)
  const planForm = reactive(emptyPaymentPlanDraft()), planFeaturesText = ref(''), saving = ref(false), saveUnknown = ref(false)
  const fields = computed(() => {
    const policy = props.plan ? permissions.value.updateFields : permissions.value.createFields
    return props.editableFields === undefined ? policy : policy === undefined ? props.editableFields : props.editableFields.filter(field => policy.includes(field))
  })
  const canEditField = (field: string) => fields.value === undefined || fields.value.includes(field)
  const canSave = computed(() => permissions.value.canManagePlans && (!!props.plan || permissions.value.canUpdateRates) && !saving.value && !saveUnknown.value)
  const selectedGroupInfo = computed(() => props.groups.find(group => group.id === planForm.group_id) ?? null)
  const groupOptions = computed(() => props.groups.filter(group => group.subscription_type === 'subscription').map(group => ({
    value: group.id, label: group.name + ' — ' + group.platform + ' (' + group.rate_multiplier + 'x)', platform: group.platform,
  })))
  const subscriptionCnyPreview = computed(() => paymentPlanCnyPreview(planForm.price, props.paymentConfig))
  const validityUnitOptions = computed(() => ['days', 'weeks', 'months'].map(value => ({ value, label: t('payment.admin.' + value) })))
  watch(context, () => {
    saving.value = false; saveUnknown.value = false
    Object.assign(planForm, paymentPlanDraft(props.show ? props.plan : null))
    planFeaturesText.value = props.show && props.plan ? parsePaymentPlanFeatures(props.plan.features).join('\n') : ''
  }, { immediate: true, flush: 'sync' })
  async function handleSavePlan(): Promise<void> {
    if (!canSave.value) return
    if (!planForm.group_id) { services.notifyError(t('payment.admin.groupRequired')); return }
    if (!Number.isFinite(planForm.price) || planForm.price <= 0) { services.notifyError(t('payment.admin.priceRequired')); return }
    if (!Number.isFinite(planForm.validity_days) || planForm.validity_days < 1) { services.notifyError(t('payment.admin.validityRequired')); return }
    const ticket = request.begin(), id = props.plan?.id
    if (!ticket.isCurrent()) return
    const payload = buildPaymentPlanPayload({ ...planForm }, planFeaturesText.value, fields.value)
    saving.value = true
    try {
      if (id !== undefined) await adminPaymentAPI.updatePlan(id, payload, ticket)
      else await adminPaymentAPI.createPlan(payload, ticket)
      if (!ticket.isCurrent()) return
      services.notifySuccess(t('common.saved')); onSaved()
    } catch (error) {
      if (!ticket.isCurrent()) return
      saveUnknown.value = paymentSubmissionMayBeUnknown(error)
      services.notifyError(extractApiErrorMessage(error, t('common.error')))
    } finally { if (ticket.isCurrent()) saving.value = false }
  }
  return { planForm, planFeaturesText, saving, saveUnknown, canSave, canEditField, selectedGroupInfo, groupOptions, subscriptionCnyPreview, validityUnitOptions, handleSavePlan }
}
