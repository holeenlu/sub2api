import type { PaymentOrder, OrderStatus } from '../api/types'
import type { PublicOrderVerifyResult } from '../api/payment'
export type ResolvedOrder = PaymentOrder | PublicOrderVerifyResult
export function normalizeOrderStatus(status: string | null | undefined): string { return String(status ?? '').trim().toUpperCase() }
export function isSuccessStatus(status: string | null | undefined): boolean { return ['COMPLETED', 'PAID', 'RECHARGING'].includes(normalizeOrderStatus(status)) }
export function isPendingStatus(status: string | null | undefined): boolean { return ['PENDING', 'CREATED', 'WAITING', 'PROCESSING'].includes(normalizeOrderStatus(status)) }
export function displayOrderStatus(status: string): OrderStatus { return normalizeOrderStatus(status) as OrderStatus }
export function hasOrderId(order: ResolvedOrder | null): order is PaymentOrder { return !!order && 'id' in order && Number.isFinite(order.id) }
export function hasAmountFields(order: ResolvedOrder | null): order is PaymentOrder {
  return !!order && 'pay_amount' in order && Number.isFinite(order.pay_amount) && 'amount' in order && Number.isFinite(order.amount)
}
export function hasPaymentType(order: ResolvedOrder | null): order is PaymentOrder { return !!order && 'payment_type' in order && typeof order.payment_type === 'string' && order.payment_type.trim() !== '' }
export function orderBonusAmount(order: ResolvedOrder | null): number {
  const value = order && 'bonus_amount' in order ? order.bonus_amount : undefined
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : 0
}
