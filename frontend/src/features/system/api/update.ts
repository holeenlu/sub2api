/** A complete release is installed by the restricted host updater. */
import { apiClient } from '@/shared/http'

export interface OnlineUpdateBuild {
  version: string
  commit: string
}

export interface OnlineUpdateTarget extends OnlineUpdateBuild {
  release_url: string
  requires_manual_migration: boolean
}

export interface OnlineUpdateJob {
  id: string
  state: 'running' | 'succeeded' | 'failed' | 'manual_intervention_required'
  phase: string
  message: string
  target_commit: string
  started_at: string
  finished_at?: string
}

export interface OnlineUpdateStatus {
  enabled: boolean
  repository: string
  branch: string
  current: OnlineUpdateBuild
  available: boolean
  target?: OnlineUpdateTarget
  job?: OnlineUpdateJob
  message?: string
}

type RequestOptions = { signal?: AbortSignal }

export async function getOnlineUpdate(options?: RequestOptions): Promise<OnlineUpdateStatus> {
  const { data } = await apiClient.get<OnlineUpdateStatus>('/admin/system/update', options)
  return data
}

export async function checkOnlineUpdate(options?: RequestOptions): Promise<OnlineUpdateStatus> {
  const { data } = await apiClient.post<OnlineUpdateStatus>('/admin/system/update/check', {}, { ...options, timeout: 130000 })
  return data
}

export async function startOnlineUpdate(targetCommit: string, operationKey: string, options?: RequestOptions): Promise<OnlineUpdateStatus> {
  const { data } = await apiClient.post<OnlineUpdateStatus>('/admin/system/update', { target_commit: targetCommit }, {
    ...options, timeout: 130000, headers: { 'Idempotency-Key': operationKey }
  })
  return data
}
