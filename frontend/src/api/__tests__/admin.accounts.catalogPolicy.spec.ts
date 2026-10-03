import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CreateAccountRequest } from '@/types'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import { create, batchCreate } from '@/api/admin/accounts'

describe('account model mapping payload', () => {
  beforeEach(() => post.mockReset().mockResolvedValue({ data: { id: 1 } }))
  it('preserves whitelist identities and aliases in both create paths', async () => {
    const form: CreateAccountRequest = {
      name: 'test', platform: 'openai', type: 'apikey',
      credentials: { api_key: 'test', model_mapping: { allowed: 'allowed', alias: 'target' } }
    }
    await create(form)
    expect(post).toHaveBeenCalledWith('/admin/accounts', form)
    await batchCreate([form])
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/batch', { accounts: [form] })
    expect(form.credentials.model_mapping).toEqual({ allowed: 'allowed', alias: 'target' })
    expect(post.mock.calls[0][1]).not.toHaveProperty('model_catalog_policy')
  })
})
