import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
/**
 * Admin Payment API endpoints
 * Handles payment management operations for administrators
 */

import { apiClient } from '@/shared/http'
import type { PaymentRequestOptions } from './payment'
function requireCurrent(options?: PaymentRequestOptions): void {
  if (options?.signal?.aborted || options?.isCurrent?.() === false) throw new DOMException('Payment context changed', 'AbortError')
}
import type {
  DashboardStats,
  PaymentOrder,
  PaymentChannel,
  ProviderInstance
} from './types'
import type { BasePaginationResponse } from '@/shared/http/requestTypes';

/** Admin-facing payment config returned by GET /admin/payment/config */
export interface AdminPaymentConfig {
  enabled: boolean
  min_amount: number
  max_amount: number
  daily_limit: number
  order_timeout_minutes: number
  max_pending_orders: number
  enabled_payment_types: string[]
  balance_disabled: boolean
  balance_recharge_multiplier: number
  subscription_usd_to_cny_rate: number
  recharge_fee_rate: number
  load_balance_strategy: string
  product_name_prefix: string
  product_name_suffix: string
  help_image_url: string
  help_text: string
}

export interface RefundResult {
  success: boolean
  warning?: string
  require_force?: boolean
  balance_deducted?: number
  subscription_days_deducted?: number
}

export const adminPaymentAPI = {
  // ==================== Config ====================

  /** Get payment configuration (admin view) */
  getConfig(options?: PaymentRequestOptions) {
    return apiClient.get<AdminPaymentConfig>('/admin/payment/config', { signal: options?.signal })
  },

  // ==================== Dashboard ====================

  /** Get payment dashboard statistics */
  getDashboard(days?: number, options?: PaymentRequestOptions) {
    return apiClient.get<DashboardStats>('/admin/payment/dashboard', {
      params: days ? { days } : undefined, signal: options?.signal
    })
  },

  // ==================== Orders ====================

  /** Get all orders (paginated, with filters) */
  getOrders(params?: {
    page?: number
    page_size?: number
    status?: string
    payment_type?: string
    user_id?: number
    keyword?: string
    start_date?: string
    end_date?: string
    order_type?: string
  }, options?: PaymentRequestOptions) {
    return apiClient.get<BasePaginationResponse<PaymentOrder>>('/admin/payment/orders', { params, signal: options?.signal })
  },

  /** Get a specific order by ID */
  getOrder(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.get<PaymentOrder>(`/admin/payment/orders/${id}`, { signal: options?.signal })
  },

  /** Cancel an order (admin) */
  cancelOrder(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post(`/admin/payment/orders/${id}/cancel`, undefined, { signal: options?.signal })
  },

  /** Retry recharge for a failed order */
  retryRecharge(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post(`/admin/payment/orders/${id}/retry`, undefined, { signal: options?.signal })
  },

  /** Process a refund */
  refundOrder(id: number, data: { amount: number; reason: string; deduct_balance?: boolean; force?: boolean }, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post<RefundResult>(`/admin/payment/orders/${id}/refund`, data, { signal: options?.signal })
  },

  /** Query and finalize a pending refund */
  queryRefund(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post<RefundResult>(`/admin/payment/orders/${id}/refund/query`, undefined, { signal: options?.signal })
  },

  // ==================== Channels ====================

  /** Get all payment channels */
  getChannels() {
    return apiClient.get<PaymentChannel[]>('/admin/payment/channels')
  },

  /** Create a payment channel */
  createChannel(data: Partial<PaymentChannel>) {
    return apiClient.post<PaymentChannel>('/admin/payment/channels', data)
  },

  /** Update a payment channel */
  updateChannel(id: number, data: Partial<PaymentChannel>) {
    return apiClient.put<PaymentChannel>(`/admin/payment/channels/${id}`, data)
  },

  /** Delete a payment channel */
  deleteChannel(id: number) {
    return apiClient.delete(`/admin/payment/channels/${id}`)
  },

  // ==================== Subscription Plans ====================

  /** Get all subscription plans */
  getPlans(options?: PaymentRequestOptions) {
    return apiClient.get<SubscriptionPlan[]>('/admin/payment/plans', { signal: options?.signal })
  },

  /** Create a subscription plan */
  createPlan(data: Record<string, unknown>, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post<SubscriptionPlan>('/admin/payment/plans', data, { signal: options?.signal })
  },

  /** Update a subscription plan */
  updatePlan(id: number, data: Record<string, unknown>, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.put<SubscriptionPlan>(`/admin/payment/plans/${id}`, data, { signal: options?.signal })
  },

  /** Delete a subscription plan */
  deletePlan(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.delete(`/admin/payment/plans/${id}`, { signal: options?.signal })
  },

  // ==================== Provider Instances ====================

  /** Get all provider instances */
  getProviders(options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.get<ProviderInstance[]>('/admin/payment/providers', { signal: options?.signal })
  },

  /** Create a provider instance */
  createProvider(data: Partial<ProviderInstance>, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.post<ProviderInstance>('/admin/payment/providers', data, { signal: options?.signal })
  },

  /** Update a provider instance */
  updateProvider(id: number, data: Partial<ProviderInstance>, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.put<ProviderInstance>(`/admin/payment/providers/${id}`, data, { signal: options?.signal })
  },

  /** Delete a provider instance */
  deleteProvider(id: number, options?: PaymentRequestOptions) {
    requireCurrent(options)
    return apiClient.delete(`/admin/payment/providers/${id}`, { signal: options?.signal })
  }
}

export default adminPaymentAPI
