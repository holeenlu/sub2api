import { defineComponent, reactive, ref } from 'vue'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { paymentsServicesKey } from '../services'
import { paymentPlansServicesKey, type PaymentPlanPermissions } from '../planServices'
import { createPaymentsServices } from './services'
import { createPaymentPlansServices } from './planServices'
import { deferred } from './harness'
import { usePaymentPlanEditor } from '../commands/usePaymentPlanEditor'
import { usePaymentPlans } from '../queries/usePaymentPlans'
import { buildPaymentPlanPayload, emptyPaymentPlanDraft, paymentPlanCnyPreview, parsePaymentPlanFeatures } from '../model/paymentPlanDraft'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
const api = vi.hoisted(() => ({ getPlans: vi.fn(), getConfig: vi.fn(), createPlan: vi.fn(), updatePlan: vi.fn() }))
vi.mock('../api/adminPayment', () => ({ adminPaymentAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks(); api.getConfig.mockResolvedValue({ data: {} }) })
const plan = (id = 7): SubscriptionPlan => ({ id, name: 'Pro', group_id: 3, description: 'Description', price: 9.99, original_price: 10, currency: 'USD', validity_days: 1, validity_unit: 'months', sort_order: 0, for_sale: true, features: ['one'], rate_multiplier: 1, daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, group_platform: 'openai', group_name: 'OpenAI' })
function harness<T>(setup: () => T, permissions = ref<PaymentPlanPermissions>({ canManagePlans: true, canUpdateRates: true, canReadPaymentConfig: false }), scope = ref<string | null>('session-a')) {
  const services = createPaymentsServices({ scopeKey: scope }), plansServices = createPaymentPlansServices({ permissions })
  let query!: T
  const wrapper = mount(defineComponent({ setup() { query = setup(); return () => null } }), { global: { provide: { [paymentsServicesKey as symbol]: services, [paymentPlansServicesKey as symbol]: plansServices } } })
  return { query, wrapper, services, permissions, scope }
}
describe('payment plan data contract', () => {
  it('retains currency, exact amounts, units and newline feature encoding', () => {
    const draft = { ...emptyPaymentPlanDraft(), group_id: 3, price: 9.99, currency: ' usd ', validity_unit: 'months', validity_days: 1 }
    expect(buildPaymentPlanPayload(draft, ' one \n\n two ')).toEqual({ ...draft, currency: 'USD', features: 'one\ntwo' })
    expect(parsePaymentPlanFeatures(' one \n\n two ')).toEqual(['one', 'two'])
    expect(draft.currency).toBe(' usd ')
  })
  it('retains the original rounded-base / ceil-fee CNY preview', () => {
    const preview = paymentPlanCnyPreview(9.99, { subscription_usd_to_cny_rate: 7.15, recharge_fee_rate: 2.5 })
    expect(preview?.amount).toContain('71.43'); expect(preview?.total).toContain('73.22')
    expect(paymentPlanCnyPreview(9.99, { subscription_usd_to_cny_rate: 0, recharge_fee_rate: 2.5 })).toBeNull()
  })
  it('distinguishes an empty writable-field set from an unrestricted one', () => {
    expect(buildPaymentPlanPayload(emptyPaymentPlanDraft(), '', [])).toEqual({})
    expect(buildPaymentPlanPayload(emptyPaymentPlanDraft(), '', ['name', 'for_sale'])).toEqual({ name: '', for_sale: true })
  })
})
describe('plan command ownership', () => {
  it('keeps the create permission and rate-update permission requirements', async () => {
    const props = reactive({ show: true, plan: null as SubscriptionPlan | null, groups: [] })
    const { query } = harness(() => usePaymentPlanEditor(props, vi.fn()), ref({ canManagePlans: true, canUpdateRates: false, canReadPaymentConfig: false }))
    Object.assign(query.planForm, plan())
    await query.handleSavePlan(); expect(api.createPlan).not.toHaveBeenCalled()
  })
  it('sends only the current update field whitelist and preserves the draft baseline', async () => {
    api.updatePlan.mockResolvedValue({ data: plan() })
    const original = plan(), props = reactive({ show: true, plan: original, groups: [], editableFields: ['name', 'price'] })
    const saved = vi.fn(), { query } = harness(() => usePaymentPlanEditor(props, saved), ref({ canManagePlans: true, canUpdateRates: false, canReadPaymentConfig: false, updateFields: ['name'] }))
    query.planForm.name = 'Changed'; query.planForm.price = 99.99
    await query.handleSavePlan()
    expect(api.updatePlan).toHaveBeenCalledWith(7, { name: 'Changed' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(original.name).toBe('Pro'); expect(original.price).toBe(9.99); expect(saved).toHaveBeenCalledOnce()
  })
  it('cannot finish a previous editor after switching the target', async () => {
    const first = deferred<unknown>(), second = deferred<unknown>()
    api.updatePlan.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const props = reactive({ show: true, plan: plan(), groups: [] }), saved = vi.fn()
    const { query, services } = harness(() => usePaymentPlanEditor(props, saved))
    const old = query.handleSavePlan(), oldSignal = api.updatePlan.mock.calls[0][2].signal
    props.plan = plan(8); const current = query.handleSavePlan()
    first.resolve({}); await old
    expect(oldSignal.aborted).toBe(true); expect(saved).not.toHaveBeenCalled(); expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(query.saving.value).toBe(true)
    second.resolve({}); await current; expect(saved).toHaveBeenCalledOnce()
  })
  it('does not resend a create whose response was lost', async () => {
    api.createPlan.mockRejectedValue(new Error('lost response'))
    const props = reactive({ show: true, plan: null as SubscriptionPlan | null, groups: [] })
    const { query } = harness(() => usePaymentPlanEditor(props, vi.fn()))
    Object.assign(query.planForm, plan()); await query.handleSavePlan(); await query.handleSavePlan()
    expect(api.createPlan).toHaveBeenCalledOnce(); expect(query.saveUnknown.value).toBe(true)
  })
})
describe('plan query ownership', () => {
  it('discards the old session result and its loading finalizer', async () => {
    const first = deferred<{ data: SubscriptionPlan[] }>(), second = deferred<{ data: SubscriptionPlan[] }>()
    api.getPlans.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { query, scope } = harness(usePaymentPlans)
    const old = query.loadPlans(), signal = api.getPlans.mock.calls[0][0].signal
    scope.value = 'session-b'; first.resolve({ data: [plan()] }); await old
    expect(signal.aborted).toBe(true); expect(query.plans.value).toEqual([]); expect(query.plansLoading.value).toBe(true)
    second.resolve({ data: [plan(8)] }); await flushPromises()
    expect(query.plans.value[0].id).toBe(8)
  })
})
