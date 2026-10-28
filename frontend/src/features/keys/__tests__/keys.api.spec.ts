import { beforeEach, describe, expect, it, vi } from 'vitest'
import { keysAPI } from '../api/keys'
const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { get, post, put, delete: remove } }))
beforeEach(() => { for (const mock of [get, post, put, remove]) { mock.mockReset(); mock.mockResolvedValue({ data: {} }) } })
describe('Key API preserves endpoint and payload contracts', () => {
  it('preserves the no-group filter and captured cancellation signal', async () => {
    const signal = new AbortController().signal
    await keysAPI.list(2, 50, { group_id: 0, status: 'inactive' }, { signal })
    expect(get).toHaveBeenCalledWith('/keys', { params: { page: 2, page_size: 50, group_id: 0, status: 'inactive' }, signal })
  })
  it('retains custom-key create fields and omits unset/unlimited create values', async () => {
    await keysAPI.create('Custom', 7, 'sk-custom_key-12345', ['10.0.0.1'], [], 0, 2, { rate_limit_5h: 0, rate_limit_1d: 1.25 })
    expect(post).toHaveBeenCalledWith('/keys', { name: 'Custom', group_id: 7, custom_key: 'sk-custom_key-12345', ip_whitelist: ['10.0.0.1'], expires_in_days: 2, rate_limit_1d: 1.25 }, { signal: undefined })
  })
  it('keeps explicit clears and reset flags in an update', async () => {
    const updates = { group_id: null, expires_at: '', quota: 0, ip_whitelist: [], rate_limit_5h: 0, reset_quota: true }
    await keysAPI.update(4, updates)
    expect(put).toHaveBeenCalledWith('/keys/4', updates, { signal: undefined })
  })
  it('exports explicit ids ahead of filters and imports the untouched v1 envelope', async () => {
    await keysAPI.exportData({ search: 'ignored' }, [2, 7])
    expect(get).toHaveBeenCalledWith('/keys/export', { params: { ids: '2,7' }, signal: undefined })
    const backup = { type: 'sub2api-keys' as const, version: 1 as const, keys: [{ key: 'sk-retained', status: 'disabled' }] }
    await keysAPI.importData(backup)
    expect(post).toHaveBeenCalledWith('/keys/import', { data: backup }, { signal: undefined })
  })
  it('rejects invalidated commands before any HTTP request, including import', async () => {
    const options = { isCurrent: () => false }
    const backup = { type: 'sub2api-keys' as const, version: 1 as const, keys: [{ key: 'sk-retained' }] }
    await expect(keysAPI.create('Canceled', 7, undefined, undefined, undefined, undefined, undefined, undefined, options)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(keysAPI.update(1, { status: 'inactive' }, options)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(keysAPI.delete(1, options)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(keysAPI.importData(backup, options)).rejects.toMatchObject({ name: 'AbortError' })
    expect(post).not.toHaveBeenCalled(); expect(put).not.toHaveBeenCalled(); expect(remove).not.toHaveBeenCalled()
  })
  it('retains cancellation identity for an already aborted signal', async () => {
    const controller = new AbortController(); controller.abort()
    await expect(keysAPI.update(1, { quota: 0 }, { signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
    expect(put).not.toHaveBeenCalled()
  })

  it('rejects a late mutation result when the transport ignored cancellation', async () => {
    let current = true
    let finish: (value: unknown) => void = () => undefined
    put.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const pending = keysAPI.update(1, { status: 'inactive' }, { isCurrent: () => current })
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    current = false; finish({ data: { id: 1, status: 'inactive' } }); await rejected
    expect(put).toHaveBeenCalledTimes(1)
  })

})
