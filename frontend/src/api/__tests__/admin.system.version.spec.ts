import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: mocks }))

import { getVersion } from '@/api/admin/system'

describe('read-only system version API', () => {
  beforeEach(() => { mocks.get.mockReset() })

  it('reads current build metadata without requesting any installation operation', async () => {
    const metadata = { version: 'dev', upstream_version: '0.2.17', build_commit: 'source-commit' }
    mocks.get.mockResolvedValue({ data: metadata })
    await expect(getVersion()).resolves.toEqual(metadata)
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(mocks.get).toHaveBeenCalledWith('/admin/system/version')
  })

  it('preserves missing optional metadata instead of creating a release version', async () => {
    mocks.get.mockResolvedValue({ data: { version: 'dev' } })
    await expect(getVersion()).resolves.toEqual({ version: 'dev' })
  })

  it('propagates authorization failures without manufacturing version data', async () => {
    const denied = { status: 403, code: 'PERMISSION_DENIED' }
    mocks.get.mockRejectedValue(denied)
    await expect(getVersion()).rejects.toBe(denied)
  })
})
