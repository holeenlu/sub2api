/**
 * Admin Channels API endpoints
 * Handles channel management for administrators
 */

import { apiClient } from '@/shared/http'
import type { Channel, CreateChannelRequest, UpdateChannelRequest, ModelDefaultPricing, SyncPricingModelsResult } from './types'

export type { BillingMode, Channel, ChannelModelPricing, ChannelTimePricing, ChannelTimePricingPeriod, PricingInterval, CreateChannelRequest, UpdateChannelRequest, ModelDefaultPricing, SyncPricingModelsResult, AccountStatsPricingRule } from './types'

interface PaginatedResponse<T> {
  items: T[]
  total: number
}

/**
 * List channels with pagination
 */
export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: string
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<Channel>> {
  const { data } = await apiClient.get<PaginatedResponse<Channel>>('/admin/channels', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

/**
 * Get channel by ID
 */
export async function getById(id: number): Promise<Channel> {
  const { data } = await apiClient.get<Channel>(`/admin/channels/${id}`)
  return data
}

/**
 * Create a new channel
 */
export async function create(req: CreateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.post<Channel>('/admin/channels', req)
  return data
}

/**
 * Update a channel
 */
export async function update(id: number, req: UpdateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.put<Channel>(`/admin/channels/${id}`, req)
  return data
}

/**
 * Delete a channel
 */
export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/channels/${id}`)
}

export async function getModelDefaultPricing(model: string, signal?: AbortSignal): Promise<ModelDefaultPricing> {
  const { data } = await apiClient.get<ModelDefaultPricing>('/admin/channels/model-pricing', {
    params: { model }, signal
  })
  return data
}

/**
 * Fetch the latest model names from the LiteLLM pricing catalog for the given platform
 */
export async function syncPricingModels(platform: string, signal?: AbortSignal): Promise<SyncPricingModelsResult> {
  const { data } = await apiClient.get<SyncPricingModelsResult>('/admin/channels/pricing/sync-models', {
    params: { platform }, signal
  })
  return data
}

const channelsAPI = { list, getById, create, update, remove, getModelDefaultPricing, syncPricingModels }
export default channelsAPI
