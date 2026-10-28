import { apiClient } from '@/shared/http'

export interface BackupS3Config {
  endpoint: string
  region: string
  bucket: string
  access_key_id: string
  secret_access_key?: string
  prefix: string
  force_path_style: boolean
}

export interface BackupScheduleConfig {
  enabled: boolean
  cron_expr: string
  retain_days: number
  retain_count: number
  monthly_archive?: BackupMonthlyArchiveConfig
}

export interface BackupMonthlyArchiveConfig {
  enabled: boolean
  days: number[]
  include_month_end: boolean
  retain_count: number // 0 = permanent
}

export interface BackupMonthlyArchive {
  dates: string[]
  retain_count: number
}

export interface BackupRecord {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  backup_type: string
  file_name: string
  s3_key: string
  parts?: BackupPart[]
  size_bytes: number
  triggered_by: string
  error_message?: string
  started_at: string
  finished_at?: string
  expires_at?: string
  progress?: string
  restore_status?: string
  restore_started_at?: string
  restore_error?: string
  restored_at?: string
  monthly_archive?: BackupMonthlyArchive
}

export interface BackupPart {
  index: number
  s3_key: string
  size_bytes: number
  sha256?: string
}

export interface BackupDownloadPart {
  index: number
  size_bytes: number
  url: string
}

export interface BackupDownloadResponse {
  url?: string
  parts?: BackupDownloadPart[]
}

export interface CreateBackupRequest {
  expire_days?: number
}

export interface TestS3Response {
  ok: boolean
  message: string
}

// S3 Config
export async function getS3Config(options?: { signal?: AbortSignal }): Promise<BackupS3Config> {
  const { data } = await apiClient.get<BackupS3Config>('/admin/backups/s3-config', options)
  return data
}

export async function updateS3Config(config: BackupS3Config, options?: { signal?: AbortSignal }): Promise<BackupS3Config> {
  const { data } = await apiClient.put<BackupS3Config>('/admin/backups/s3-config', config, options)
  return data
}

export async function testS3Connection(config: BackupS3Config, options?: { signal?: AbortSignal }): Promise<TestS3Response> {
  const { data } = await apiClient.post<TestS3Response>('/admin/backups/s3-config/test', config, options)
  return data
}

// Async image object storage
//
// Shares the S3 client with backups, so `reuse_backup_s3` borrows the endpoint and
// credentials configured above and only keeps its own bucket/prefix.
export interface ImageStorageConfig {
  enabled: boolean
  reuse_backup_s3: boolean
  bucket: string
  prefix: string
  public_base_url: string
  presign_expiry_hours: number
  max_download_bytes: number
  endpoint: string
  region: string
  access_key_id: string
  secret_access_key?: string
  force_path_style: boolean
}

export interface ImageStorageConfigResponse {
  config: ImageStorageConfig
  secret_configured: boolean
}

export async function getImageStorageConfig(options?: { signal?: AbortSignal }): Promise<ImageStorageConfigResponse> {
  const { data } = await apiClient.get<ImageStorageConfigResponse>('/admin/backups/image-storage', options)
  return data
}

export async function updateImageStorageConfig(
  config: ImageStorageConfig,
  options?: { signal?: AbortSignal },
): Promise<ImageStorageConfig> {
  const { data } = await apiClient.put<ImageStorageConfig>('/admin/backups/image-storage', config, options)
  return data
}

export async function testImageStorageConnection(
  config: ImageStorageConfig,
  options?: { signal?: AbortSignal },
): Promise<TestS3Response> {
  const { data } = await apiClient.post<TestS3Response>(
    '/admin/backups/image-storage/test',
    config, options)
  return data
}

// Schedule
export async function getSchedule(options?: { signal?: AbortSignal }): Promise<BackupScheduleConfig> {
  const { data } = await apiClient.get<BackupScheduleConfig>('/admin/backups/schedule', options)
  return data
}

export async function updateSchedule(config: BackupScheduleConfig, options?: { signal?: AbortSignal }): Promise<BackupScheduleConfig> {
  const { data } = await apiClient.put<BackupScheduleConfig>('/admin/backups/schedule', config, options)
  return data
}

// Backup operations
export async function createBackup(req?: CreateBackupRequest, options?: { signal?: AbortSignal }): Promise<BackupRecord> {
  const { data } = await apiClient.post<BackupRecord>('/admin/backups', req || {}, options)
  return data
}

export async function listBackups(options?: { signal?: AbortSignal }): Promise<{ items: BackupRecord[] }> {
  const { data } = await apiClient.get<{ items: BackupRecord[] }>('/admin/backups', options)
  return data
}

export async function getBackup(id: string, options?: { signal?: AbortSignal }): Promise<BackupRecord> {
  const { data } = await apiClient.get<BackupRecord>(`/admin/backups/${id}`, options)
  return data
}

export async function deleteBackup(id: string, deleteArchived = false, options?: { signal?: AbortSignal }): Promise<void> {
  await apiClient.delete(`/admin/backups/${id}`, deleteArchived ? { ...options, params: { delete_archived: true } } : options)
}

export async function getDownloadURL(id: string, options?: { signal?: AbortSignal }): Promise<BackupDownloadResponse> {
  const { data } = await apiClient.get<BackupDownloadResponse>(`/admin/backups/${id}/download-url`, options)
  return data
}

// Restore
export async function restoreBackup(id: string, password: string, options?: { signal?: AbortSignal }): Promise<BackupRecord> {
  const { data } = await apiClient.post<BackupRecord>(`/admin/backups/${id}/restore`, { password }, options)
  return data
}

export const backupAPI = {
  getS3Config,
  updateS3Config,
  testS3Connection,
  getImageStorageConfig,
  updateImageStorageConfig,
  testImageStorageConnection,
  getSchedule,
  updateSchedule,
  createBackup,
  listBackups,
  getBackup,
  deleteBackup,
  getDownloadURL,
  restoreBackup,
}

export default backupAPI
