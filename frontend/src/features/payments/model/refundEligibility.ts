import type { PaymentOrder } from '../api/types'

/** Mirrors validateRefundRequest; the server still validates ownership and available balance. */
export function canUserRequestRefund(
  order: Pick<PaymentOrder, 'order_type' | 'status' | 'provider_instance_id'>,
  eligibleProviderIds: ReadonlySet<string>
): boolean {
  return order.order_type === 'balance' && order.status === 'COMPLETED'
    && !!order.provider_instance_id && eligibleProviderIds.has(order.provider_instance_id)
}

/** PrepareRefund permits a first partial amount, but not another refund after PARTIALLY_REFUNDED.
 * REFUND_PENDING is reconciled through the query endpoint, never by resubmitting the refund.
 */
export function canRefund(status: string): boolean {
  return status === 'COMPLETED' || status === 'REFUND_REQUESTED' || status === 'REFUND_FAILED'
}
