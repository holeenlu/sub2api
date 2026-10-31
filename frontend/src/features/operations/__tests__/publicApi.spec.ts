import { existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { opsAPI, auditAPI, channelMonitorAPI, channelMonitorUserAPI, monitorAPI } from '../api'
import { opsAPI as directOps } from '../api/ops'
import { auditAPI as directAudit } from '../api/audit'
import { channelMonitorAPI as directAdminMonitor } from '../api/adminChannelMonitor'
import { channelMonitorUserAPI as directUserMonitor } from '../api/userChannelMonitor'
import { getMatrix, getConfig, updateConfig } from '../api/monitor'
import { submitBatchImageJob, cancelBatchImageJob } from '../../jobs/api'
import { submitBatchImageJob as directSubmit, cancelBatchImageJob as directCancel } from '../../jobs/api/batchImage'

describe('operations/jobs public API ownership', () => {
  it('exports the same endpoint owners without compatibility implementations', () => {
    expect(opsAPI).toBe(directOps)
    expect(auditAPI).toBe(directAudit)
    expect(channelMonitorAPI).toBe(directAdminMonitor)
    expect(channelMonitorUserAPI).toBe(directUserMonitor)
    expect(monitorAPI.getMatrix).toBe(getMatrix)
    expect(monitorAPI.getConfig).toBe(getConfig)
    expect(monitorAPI.updateConfig).toBe(updateConfig)
    expect(submitBatchImageJob).toBe(directSubmit)
    expect(cancelBatchImageJob).toBe(directCancel)
  })

  it('retires the assigned global API implementation paths', () => {
    const root = resolve(__dirname, '../../..')
    for (const file of ['api/admin/ops.ts', 'api/admin/audit.ts', 'api/admin/channelMonitor.ts',
      'api/channelMonitor.ts', 'api/channelMonitorV2.ts', 'api/batchImage.ts']) {
      expect(existsSync(resolve(root, file)), file).toBe(false)
    }
  })
})
