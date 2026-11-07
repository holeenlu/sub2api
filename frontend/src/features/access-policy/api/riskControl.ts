import type { ContentModerationConfig, TestContentModerationAPIKeysPayload, TestContentModerationAPIKeysResponse, UpdateContentModerationConfig, ContentModerationRuntimeStatus, ListContentModerationLogsParams, ContentModerationLogsResponse, ContentModerationUnbanUserResponse, DeleteFlaggedHashResponse, ClearFlaggedHashesResponse } from './types';
export type { ModerationMode, ModerationEngine, ModerationEngineMeta, KeywordBlockingMode, ContentModerationModelFilterType, ContentModerationModelFilter, ContentModerationDistillationConfig, ContentModerationDeviceGuardConfig, ContentModerationRefusalGuardConfig, ContentModerationGuards, ContentModerationConfig, ContentModerationAPIKeyStatusValue, ContentModerationAPIKeyStatus, TestContentModerationAPIKeysPayload, TestContentModerationAPIKeysResponse, ContentModerationTestAuditResult, UpdateContentModerationConfig, ContentModerationRuntimeStatus, ContentModerationAPIKeyLoad, ContentModerationLog, UpdateModerationEngineConfig, ListContentModerationLogsParams, ContentModerationLogsResponse, ContentModerationUnbanUserResponse, DeleteFlaggedHashResponse, ClearFlaggedHashesResponse } from './types'
import { apiClient } from '@/shared/http';

export async function getConfig(signal?: AbortSignal): Promise<ContentModerationConfig> {
  const { data } = await apiClient.get<ContentModerationConfig>('/admin/risk-control/config', { signal })
  return data
}

export async function updateConfig(
  payload: UpdateContentModerationConfig
): Promise<ContentModerationConfig> {
  const { data } = await apiClient.put<ContentModerationConfig>('/admin/risk-control/config', payload)
  return data
}

export async function getStatus(signal?: AbortSignal): Promise<ContentModerationRuntimeStatus> {
  const { data } = await apiClient.get<ContentModerationRuntimeStatus>('/admin/risk-control/status', { signal })
  return data
}

export async function testAPIKeys(
  payload: TestContentModerationAPIKeysPayload = {}
): Promise<TestContentModerationAPIKeysResponse> {
  const { data } = await apiClient.post<TestContentModerationAPIKeysResponse>('/admin/risk-control/api-keys/test', payload)
  return data
}

export async function listLogs(
  params: ListContentModerationLogsParams = {},
  signal?: AbortSignal
): Promise<ContentModerationLogsResponse> {
  const { data } = await apiClient.get<ContentModerationLogsResponse>('/admin/risk-control/logs', {
    params, signal,
  })
  return data
}

export async function unbanUser(userID: number): Promise<ContentModerationUnbanUserResponse> {
  const { data } = await apiClient.post<ContentModerationUnbanUserResponse>(
    `/admin/risk-control/users/${userID}/unban`
  )
  return data
}

export async function deleteFlaggedHash(inputHash: string): Promise<DeleteFlaggedHashResponse> {
  const { data } = await apiClient.delete<DeleteFlaggedHashResponse>('/admin/risk-control/hashes', {
    data: { input_hash: inputHash },
  })
  return data
}

export async function clearFlaggedHashes(): Promise<ClearFlaggedHashesResponse> {
  const { data } = await apiClient.delete<ClearFlaggedHashesResponse>('/admin/risk-control/hashes/all')
  return data
}

export const riskControlAPI = {
  getConfig,
  updateConfig,
  getStatus,
  testAPIKeys,
  listLogs,
  unbanUser,
  deleteFlaggedHash,
  clearFlaggedHashes,
}

export default riskControlAPI
