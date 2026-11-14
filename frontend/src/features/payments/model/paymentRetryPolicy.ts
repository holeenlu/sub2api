import { normalizeVisibleMethod } from './paymentFlow'
/** Only explicit merchant/configuration rejection is safe for the existing desktop-QR fallback.
 * A network/gateway/JSAPI failure or an unrecognized response may already have created or paid an order.
 */
export function canFallbackToDesktopQr(error: unknown, method: string, mobile: boolean, attempted: boolean): boolean {
  if (!mobile || attempted || normalizeVisibleMethod(method) !== 'wxpay') return false
  if (typeof error !== 'object' || error === null || !('reason' in error)) return false
  return error.reason === 'WECHAT_H5_NOT_AUTHORIZED' || error.reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
}
export function paymentSubmissionMayBeUnknown(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return true
  const reason = 'reason' in error && typeof error.reason === 'string' ? error.reason : ''
  if (['WECHAT_H5_NOT_AUTHORIZED', 'WECHAT_PAYMENT_MP_NOT_CONFIGURED', 'TOO_MANY_PENDING', 'CANCEL_RATE_LIMITED', 'INVALID_AMOUNT', 'INVALID_ORDER_TYPE', 'BALANCE_PAYMENT_DISABLED'].includes(reason)) return false
  const status = 'status' in error && typeof error.status === 'number' ? error.status : 0
  return status === 0 || status >= 500
}

/** Stripe's definite input/card rejection permits correction; other outcomes require an order read. */
export function stripeConfirmationMayBeUnknown(error: unknown): boolean {
  return typeof error !== 'object' || error === null || !('type' in error)
    || typeof error.type !== 'string'
    || !['card_error', 'validation_error', 'invalid_request_error'].includes(error.type)
}
