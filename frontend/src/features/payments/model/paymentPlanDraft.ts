import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import type { AdminPaymentConfig } from '../api/adminPayment'
import { formatPaymentAmount } from './currency'
export interface PaymentPlanDraft {
  name: string; group_id: number | null; description: string; price: number; original_price: number;
  currency: string; validity_days: number; validity_unit: string; sort_order: number; for_sale: boolean
}
export function emptyPaymentPlanDraft(): PaymentPlanDraft {
  return { name: '', group_id: null, description: '', price: 0, original_price: 0, currency: '', validity_days: 30, validity_unit: 'days', sort_order: 0, for_sale: true }
}
export function paymentPlanDraft(plan: SubscriptionPlan | null): PaymentPlanDraft {
  if (!plan) return emptyPaymentPlanDraft()
  return { name: plan.name, group_id: plan.group_id, description: plan.description, price: plan.price,
    original_price: plan.original_price || 0, currency: plan.currency || '', validity_days: plan.validity_days,
    validity_unit: plan.validity_unit || 'days', sort_order: plan.sort_order || 0, for_sale: plan.for_sale }
}
export function parsePaymentPlanFeatures(features: string | string[] | null | undefined): string[] {
  return typeof features === 'string' ? features.split('\n').map(value => value.trim()).filter(Boolean) : [...(features ?? [])]
}
export function buildPaymentPlanPayload(draft: PaymentPlanDraft, featuresText: string, editableFields?: readonly string[]): Record<string, unknown> {
  const full = { ...draft, original_price: draft.original_price || 0, currency: draft.currency.trim().toUpperCase(),
    features: featuresText.split('\n').map(value => value.trim()).filter(Boolean).join('\n') }
  return Object.fromEntries(Object.entries(full).filter(([field]) => editableFields === undefined || editableFields.includes(field)))
}
/** Display-only preview retains the existing round-base / ceil-fee CNY rules. */
export function paymentPlanCnyPreview(price: number, config?: Pick<AdminPaymentConfig, 'subscription_usd_to_cny_rate' | 'recharge_fee_rate'> | null) {
  const rate = Number(config?.subscription_usd_to_cny_rate) || 0
  if (!Number.isFinite(price) || price <= 0 || rate <= 0) return null
  const amount = Math.round(price * rate * 100) / 100
  const feeRate = Number(config?.recharge_fee_rate) || 0
  const fee = feeRate > 0 ? Math.ceil(((amount * feeRate) / 100) * 100) / 100 : 0
  const total = feeRate > 0 ? Math.round((amount + fee) * 100) / 100 : amount
  return { amount: formatPaymentAmount(amount, 'CNY'), feeRate, total: formatPaymentAmount(total, 'CNY') }
}
