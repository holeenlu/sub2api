import { apiClient, type HttpRequestConfig } from '@/shared/http'
import type { BasePaginationResponse } from '@/shared/http/requestTypes';
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import type { PaymentConfig, MethodLimitsResponse, CheckoutInfoResponse, CreateOrderRequest, CreateOrderResult, PaymentOrder } from './types'
export interface PublicOrderVerifyResult {
  out_trade_no: string; status: string; paid: boolean; created_at: string; expires_at: string
}
export interface PaymentRequestOptions { signal?: AbortSignal; isCurrent?: () => boolean }
function requireCurrent(options?: PaymentRequestOptions): void {
  if (options?.signal?.aborted || options?.isCurrent?.() === false) throw new DOMException('Payment context changed', 'AbortError')
}
export const paymentAPI = {
  getConfig(options?: PaymentRequestOptions) { return apiClient.get<PaymentConfig>('/payment/config', { signal: options?.signal }) },
  getPlans(options?: PaymentRequestOptions) { return apiClient.get<SubscriptionPlan[]>('/payment/plans', { signal: options?.signal }) },
  getCheckoutInfo(options?: PaymentRequestOptions) { return apiClient.get<CheckoutInfoResponse>('/payment/checkout-info', { signal: options?.signal }) },
  getLimits(options?: PaymentRequestOptions) { return apiClient.get<MethodLimitsResponse>('/payment/limits', { signal: options?.signal }) },
  async createOrder(data: CreateOrderRequest, options?: PaymentRequestOptions) {
    requireCurrent(options)
    const response = await apiClient.post<CreateOrderResult>('/payment/orders', data, { signal: options?.signal })
    requireCurrent(options)
    return response
  },
  getMyOrders(params?: { page?: number; page_size?: number; status?: string }, options?: PaymentRequestOptions) {
    return apiClient.get<BasePaginationResponse<PaymentOrder>>('/payment/orders/my', { params, signal: options?.signal })
  },
  getOrder(id: number, options?: PaymentRequestOptions) {
    return apiClient.get<PaymentOrder>('/payment/orders/' + id, { signal: options?.signal })
  },
  async cancelOrder(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    const response = await apiClient.post('/payment/orders/' + id + '/cancel', undefined, { signal: options?.signal })
    requireCurrent(options); return response
  },
  verifyOrder(outTradeNo: string, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post<PaymentOrder>('/payment/orders/verify', { out_trade_no: outTradeNo }, { signal: options?.signal })
  },
  /** Public order credentials never inherit login credentials, scope, or auth recovery. */
  verifyOrderPublic(outTradeNo: string, options?: PaymentRequestOptions) {
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous', signal: options?.signal }
    return apiClient.post<PublicOrderVerifyResult>('/payment/public/orders/verify', { out_trade_no: outTradeNo }, config)
  },
  resolveOrderPublicByResumeToken(resumeToken: string, options?: PaymentRequestOptions) {
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous', signal: options?.signal }
    return apiClient.post<PublicOrderVerifyResult>('/payment/public/orders/resolve', { resume_token: resumeToken }, config)
  },
  async requestRefund(id: number, data: { reason: string }, options?: PaymentRequestOptions) {
    requireCurrent(options)
    const response = await apiClient.post('/payment/orders/' + id + '/refund-request', data, { signal: options?.signal })
    requireCurrent(options); return response
  },
  getRefundEligibleProviders(options?: PaymentRequestOptions) {
    return apiClient.get<{ provider_instance_ids: string[] }>('/payment/orders/refund-eligible-providers', { signal: options?.signal })
  }
}
