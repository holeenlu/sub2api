import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as accessKeys from '../adminAccessKeys'
import { adminComplianceAPI } from '../compliance'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))
beforeEach(() => vi.resetAllMocks())

const input: accessKeys.AdminKeyInput = {
  name: 'Scoped operator', description: '', enabled: true, expires_at: null, allowed_ips: [],
  policy: { global_permissions: [], rules: [{ permissions: ['groups.models.read'], group_ids: [7], all_groups: false, include_ungrouped: false, platforms: [], account_types: [] }] },
}

describe('management access-key wire contract', () => {
  it('preserves the list response and passes an optional cancellation signal', async () => {
    const data = { items: [], permissions: [] }
    http.get.mockResolvedValue({ data })
    await expect(accessKeys.list()).resolves.toBe(data)
    expect(http.get).toHaveBeenLastCalledWith('/admin/settings/admin-api-keys')
    const signal = new AbortController().signal
    await expect(accessKeys.list({ signal })).resolves.toBe(data)
    expect(http.get).toHaveBeenLastCalledWith('/admin/settings/admin-api-keys', { signal })
  })

  it('creates and updates exactly the selected policy without expanding grants', async () => {
    const created = { key: 'test-only-secret', item: { id: 'key/1', ...input } }
    http.post.mockResolvedValueOnce({ data: created })
    http.put.mockResolvedValueOnce({ data: created.item })
    await expect(accessKeys.create(input)).resolves.toBe(created)
    await expect(accessKeys.update('key/1', input)).resolves.toBe(created.item)
    expect(http.post).toHaveBeenCalledWith('/admin/settings/admin-api-keys', input)
    expect(http.put).toHaveBeenCalledWith('/admin/settings/admin-api-keys/key%2F1', input)
    expect(input.policy.global_permissions).toEqual([])
    expect(input.policy.rules[0].permissions).toEqual(['groups.models.read'])
  })

  it('keeps rotation, revocation and deletion as separate server commands', async () => {
    const rotated = { key: 'rotated-test-secret', item: { id: 'key/1', ...input } }
    http.post.mockResolvedValueOnce({ data: rotated })
    http.delete.mockResolvedValue({ data: { message: 'ok' } })
    await expect(accessKeys.rotate('key/1')).resolves.toBe(rotated)
    await accessKeys.revoke('key/1')
    await accessKeys.deleteRecord('key/1')
    expect(http.post).toHaveBeenCalledWith('/admin/settings/admin-api-keys/key%2F1/rotate')
    expect(http.delete.mock.calls).toEqual([
      ['/admin/settings/admin-api-keys/key%2F1'], ['/admin/settings/admin-api-keys/key%2F1/record'],
    ])
  })
})

describe('explicit admin compliance requests', () => {
  it('does not acknowledge anything while loading status', async () => {
    const status = { required: true, ack_phrase_en: 'Exact server phrase' }
    http.get.mockResolvedValue({ data: status })
    await expect(adminComplianceAPI.getStatus()).resolves.toBe(status)
    expect(http.get).toHaveBeenCalledWith('/admin/compliance')
    const signal = new AbortController().signal
    await adminComplianceAPI.getStatus({ signal })
    expect(http.get).toHaveBeenLastCalledWith('/admin/compliance', { signal })
    expect(http.post).not.toHaveBeenCalled()
  })

  it('submits only the supplied manual phrase and language', async () => {
    const status = { required: false, version: 'server-version' }
    http.post.mockResolvedValueOnce({ data: status })
    const payload = { phrase: 'Manual test phrase', language: 'en' }
    await expect(adminComplianceAPI.accept(payload)).resolves.toBe(status)
    expect(http.post).toHaveBeenCalledWith('/admin/compliance/accept', payload)
  })
})
