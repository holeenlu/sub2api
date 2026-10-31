import { beforeEach, describe, expect, it, vi } from 'vitest'
import { adminKeysAPI } from '../admin'
const { put } = vi.hoisted(() => ({ put: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { put } }))
beforeEach(() => put.mockReset())

describe('ordinary user API-key group administration', () => {
  it.each([[7, 7], [null, 0]] as const)('serializes group %s as %s without changing key type', async (selected, serialized) => {
    const response = { api_key: { id: 42, group_id: selected }, auto_granted_group_access: selected !== null, granted_group_id: selected ?? undefined, granted_group_name: selected === null ? undefined : 'Allowed group' }
    put.mockResolvedValueOnce({ data: response })
    await expect(adminKeysAPI.updateApiKeyGroup(42, selected)).resolves.toBe(response)
    expect(put).toHaveBeenCalledWith('/admin/api-keys/42', { group_id: serialized })
  })

  it.each([[7, 7], [null, 0]] as const)('forwards the signal and preserves group %s as %s', async (selected, serialized) => {
    const options = { signal: new AbortController().signal }
    const response = { api_key: { id: 42, group_id: selected }, auto_granted_group_access: false }
    put.mockResolvedValueOnce({ data: response })
    await expect(adminKeysAPI.updateApiKeyGroup(42, selected, options)).resolves.toBe(response)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/admin/api-keys/42', { group_id: serialized }, options)
  })
})
