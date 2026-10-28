import { beforeEach, describe, expect, it, vi } from 'vitest'
import { backupAPI as publicAPI } from '..'
import { backupAPI as apiEntry } from '../api'
import backup, { backupAPI, type BackupS3Config, type ImageStorageConfig } from '../api/backup'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

const s3: BackupS3Config = { endpoint: 'https://storage.example.invalid', region: 'fixture', bucket: 'backups', access_key_id: 'fixture-key', prefix: 'backups/', force_path_style: false }
const imageStorage: ImageStorageConfig = { ...s3, enabled: true, reuse_backup_s3: true, bucket: 'images', prefix: 'images/', public_base_url: '', presign_expiry_hours: 24, max_download_bytes: 0 }

describe('system backup API transport', () => {
  beforeEach(() => { vi.resetAllMocks() })

  it('publishes the same implementation through both public entries and the module default', () => {
    expect(publicAPI).toBe(backupAPI)
    expect(apiEntry).toBe(backupAPI)
    expect(backup).toBe(backupAPI)
  })

  it.each([undefined, ''])('preserves omitted versus explicitly empty S3 secrets (%s)', async secret => {
    const config = secret === undefined ? { ...s3 } : { ...s3, secret_access_key: secret }
    http.get.mockResolvedValue({ data: config })
    http.put.mockResolvedValue({ data: config })
    http.post.mockResolvedValue({ data: { ok: false, message: 'connection rejected' } })
    expect(await backupAPI.getS3Config()).toBe(config)
    expect(await backupAPI.updateS3Config(config)).toBe(config)
    expect(await backupAPI.testS3Connection(config)).toEqual({ ok: false, message: 'connection rejected' })
    expect(http.get).toHaveBeenCalledWith('/admin/backups/s3-config', undefined)
    expect(http.put).toHaveBeenCalledWith('/admin/backups/s3-config', config, undefined)
    expect(http.post).toHaveBeenCalledWith('/admin/backups/s3-config/test', config, undefined)
    expect(http.put.mock.calls[0][1]).toBe(config)
    expect(Object.prototype.hasOwnProperty.call(http.put.mock.calls[0][1], 'secret_access_key')).toBe(secret !== undefined)
  })

  it('keeps image storage config and secret_configured server facts separate', async () => {
    const response = { config: imageStorage, secret_configured: true }
    http.get.mockResolvedValue({ data: response })
    http.put.mockResolvedValue({ data: imageStorage })
    http.post.mockResolvedValue({ data: { ok: false, message: 'bucket denied' } })
    expect(await backupAPI.getImageStorageConfig()).toBe(response)
    expect(await backupAPI.updateImageStorageConfig(imageStorage)).toBe(imageStorage)
    expect(await backupAPI.testImageStorageConnection(imageStorage)).toEqual({ ok: false, message: 'bucket denied' })
    expect(http.get).toHaveBeenCalledWith('/admin/backups/image-storage', undefined)
    expect(http.put).toHaveBeenCalledWith('/admin/backups/image-storage', imageStorage, undefined)
    expect(http.post).toHaveBeenCalledWith('/admin/backups/image-storage/test', imageStorage, undefined)
    expect(imageStorage).not.toHaveProperty('secret_access_key')
  })

  it('retains disabled schedules, month-end archives and permanent retention zero', async () => {
    const config = { enabled: false, cron_expr: '', retain_days: 0, retain_count: 0, monthly_archive: { enabled: true, days: [1, 31], include_month_end: true, retain_count: 0 } }
    http.get.mockResolvedValue({ data: config })
    http.put.mockResolvedValue({ data: config })
    expect(await backupAPI.getSchedule()).toBe(config)
    expect(await backupAPI.updateSchedule(config)).toBe(config)
    expect(http.get).toHaveBeenCalledWith('/admin/backups/schedule', undefined)
    expect(http.put).toHaveBeenCalledWith('/admin/backups/schedule', config, undefined)
  })

  it.each([undefined, { expire_days: 0 }])('preserves the existing optional create body %j and pending state', async request => {
    const record = { id: 'fixture', status: 'pending', progress: 'queued' }
    http.post.mockResolvedValue({ data: record })
    expect(await backupAPI.createBackup(request)).toBe(record)
    expect(http.post).toHaveBeenCalledWith('/admin/backups', request ?? {}, undefined)
  })

  it('returns backup, part checksums and restore state without normalization', async () => {
    const record = { id: 'fixture', status: 'completed', restore_status: 'failed', restore_error: 'fixture error', parts: [{ index: 1, s3_key: 'part-1', size_bytes: 0, sha256: 'fixture-checksum' }] }
    const response = { items: [record] }
    http.get.mockResolvedValueOnce({ data: response }).mockResolvedValueOnce({ data: record })
    expect(await backupAPI.listBackups()).toBe(response)
    expect(await backupAPI.getBackup('fixture')).toBe(record)
    expect(http.get.mock.calls).toEqual([['/admin/backups', undefined], ['/admin/backups/fixture', undefined]])
  })

  it.each([false, true])('sends archived deletion permission only when explicitly requested: %s', async archived => {
    await backupAPI.deleteBackup('fixture', archived)
    expect(http.delete).toHaveBeenCalledWith('/admin/backups/fixture', archived ? { params: { delete_archived: true } } : undefined)
  })

  it.each([{ url: 'https://download.example.invalid/backup' }, { parts: [{ index: 2, size_bytes: 12, url: 'https://download.example.invalid/part-2' }] }])('preserves existing single or multipart download responses', async response => {
    http.get.mockResolvedValue({ data: response })
    expect(await backupAPI.getDownloadURL('fixture')).toBe(response)
    expect(http.get).toHaveBeenCalledWith('/admin/backups/fixture/download-url', undefined)
  })

  it('submits restore password unchanged and does not report running restoration as complete', async () => {
    const response = { id: 'fixture', status: 'completed', restore_status: 'running' }
    http.post.mockResolvedValue({ data: response })
    expect(await backupAPI.restoreBackup('fixture', ' fixture password ')).toBe(response)
    expect(http.post).toHaveBeenCalledWith('/admin/backups/fixture/restore', { password: ' fixture password ' }, undefined)
  })

  it.each(['PERMISSION_DENIED', 'STEP_UP_REQUIRED', 'STEP_UP_ADMIN_API_KEY_FORBIDDEN', 'STEP_UP_NOT_ENABLED'])('preserves %s without retrying restoration', async code => {
    const error = { response: { status: 403, data: { code } } }
    http.post.mockRejectedValue(error)
    await expect(backupAPI.restoreBackup('fixture', 'fixture')).rejects.toBe(error)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.get).not.toHaveBeenCalled()
  })
})
