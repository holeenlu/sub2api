import { apiClient } from '@/shared/http'
import type {
  PromptAuditConfig,
  PromptAuditEvent,
  PromptAuditRuntime,
  PromptAuditUpdateRequest,
  PromptDeletePreview,
  PromptDeleteResult,
  PromptEventFilters,
  PromptEventPage,
  PromptProbeResult,
  PromptAuditEndpointDraft,
} from './types'
import { eventFilterPayload, eventQueryParams } from './viewModel'

const basePath = '/admin/prompt-audit'

export async function getConfig(options: { signal?: AbortSignal } = {}): Promise<PromptAuditConfig> {
  const { data } = await apiClient.get<PromptAuditConfig>(`${basePath}/config`, { signal: options.signal })
  return data
}

export async function updateConfig(payload: PromptAuditUpdateRequest, options: { signal?: AbortSignal } = {}): Promise<PromptAuditConfig> {
  const { data } = await apiClient.put<PromptAuditConfig>(`${basePath}/config`, payload, { signal: options.signal })
  return data
}

export async function probeEndpoint(endpoint: PromptAuditEndpointDraft, options: { signal?: AbortSignal } = {}): Promise<PromptProbeResult> {
  const { data } = await apiClient.post<PromptProbeResult>(`${basePath}/endpoints/probe`, {
    endpoint: {
      id: endpoint.id,
      name: endpoint.name,
      protocol: 'openai_compatible',
      base_url: endpoint.base_url,
      model: endpoint.model,
      token: endpoint.token || undefined,
      timeout_ms: endpoint.timeout_ms,
      input_limit: endpoint.input_limit,
      enabled: endpoint.enabled,
    },
  }, { signal: options.signal })
  return data
}

export async function getRuntime(options: { signal?: AbortSignal } = {}): Promise<PromptAuditRuntime> {
  const { data } = await apiClient.get<PromptAuditRuntime>(`${basePath}/runtime`, { signal: options.signal })
  return data
}

export async function listEvents(
  filters: PromptEventFilters,
  page: number,
  pageSize: number, options: { signal?: AbortSignal } = {}
): Promise<PromptEventPage> {
  const { data } = await apiClient.get<PromptEventPage>(`${basePath}/events`, {
    signal: options.signal,
    params: { page, page_size: pageSize, ...eventQueryParams(filters) },
  })
  return data
}

export async function getEvent(id: number, options: { signal?: AbortSignal } = {}): Promise<PromptAuditEvent> {
  const { data } = await apiClient.get<PromptAuditEvent>(`${basePath}/events/${id}`, { signal: options.signal })
  return data
}

export async function deleteEvent(id: number, options: { signal?: AbortSignal } = {}): Promise<PromptDeleteResult> {
  const { data } = await apiClient.delete<PromptDeleteResult>(`${basePath}/events/${id}`, { signal: options.signal })
  return data
}

export async function batchDeleteEvents(ids: number[], options: { signal?: AbortSignal } = {}): Promise<PromptDeleteResult> {
  const { data } = await apiClient.post<PromptDeleteResult>(`${basePath}/events/batch-delete`, { ids }, { signal: options.signal })
  return data
}

export async function previewDelete(filters: PromptEventFilters, options: { signal?: AbortSignal } = {}): Promise<PromptDeletePreview> {
  const { data } = await apiClient.post<PromptDeletePreview>(
    `${basePath}/events/delete-preview`,
    eventFilterPayload(filters), { signal: options.signal }
  )
  return data
}

export async function deleteEventsByFilter(
  filters: PromptEventFilters,
  preview: PromptDeletePreview, options: { signal?: AbortSignal } = {}
): Promise<PromptDeleteResult> {
  const { data } = await apiClient.post<PromptDeleteResult>(`${basePath}/events/delete-by-filter`, {
    filter: eventFilterPayload(filters),
    snapshot_max_id: preview.snapshot_max_id,
    filter_hash: preview.filter_hash,
    confirmation_token: preview.confirmation_token,
    confirm: true,
  }, { signal: options.signal })
  return data
}

export const promptAuditAPI = {
  getConfig,
  updateConfig,
  probeEndpoint,
  getRuntime,
  listEvents,
  getEvent,
  deleteEvent,
  batchDeleteEvents,
  previewDelete,
  deleteEventsByFilter,
}

export default promptAuditAPI
