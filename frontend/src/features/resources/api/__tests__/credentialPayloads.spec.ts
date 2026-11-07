import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { batchUpdateCredentials } from '../accounts'
import { refreshAntigravityToken } from '../antigravity'

vi.mock('@/shared/http', () => ({ apiClient: { post: vi.fn() } }))
beforeEach(() => vi.resetAllMocks())

describe('resource credential API payloads', () => {
  it.each([undefined, null, 0, 7])('keeps Antigravity refresh-token and proxy %s semantics', async proxyId => {
    const token = { access_token: 'access', refresh_token: 'rotated' }
    vi.mocked(apiClient.post).mockResolvedValueOnce({ data: token })
    expect(await refreshAntigravityToken(' refresh-token ', proxyId)).toBe(token)
    expect(apiClient.post).toHaveBeenCalledWith('/admin/antigravity/oauth/refresh-token', {
      refresh_token: ' refresh-token ', ...(proxyId ? { proxy_id: proxyId } : {})
    })
  })

  it.each([
    { field: 'intercept_warmup_requests', value: false },
    { field: 'intercept_warmup_requests', value: true },
    { field: 'account_uuid', value: '' },
    { field: 'account_uuid', value: null },
    { field: 'org_uuid', value: 'organization-id' }
  ])('passes the credential value through unchanged: $field=$value', async ({ field, value }) => {
    const result = { success: 1, failed: 0, results: [{ account_id: 42, success: true }] }
    vi.mocked(apiClient.post).mockResolvedValueOnce({ data: result })
    const request = { account_ids: [42], field, value }
    expect(await batchUpdateCredentials(request)).toBe(result)
    expect(apiClient.post).toHaveBeenCalledWith('/admin/accounts/batch-update-credentials', request)
    expect(vi.mocked(apiClient.post).mock.calls[0]?.[1]).toBe(request)
  })
})
