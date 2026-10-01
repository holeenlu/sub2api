import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CreateAccountRequest } from '@/types'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import { create, batchCreate } from '@/api/admin/accounts'

const input = (overrides: Partial<CreateAccountRequest> = {}): CreateAccountRequest => ({
  name: 'test', platform: 'openai', type: 'apikey',
  credentials: { api_key: 'test', model_mapping: { allowed: 'allowed', alias: 'target' } },
  ...overrides
})

describe('account creation access policy', () => {
  beforeEach(() => post.mockReset().mockResolvedValue({ data: { id: 1 } }))

  it('saves explicit allowed names and aliases together without mutating the form', async () => {
    const form = input()
    await create(form)
    expect(post).toHaveBeenCalledWith('/admin/accounts', {
      ...form,
      model_catalog_policy: { models: ['allowed', 'alias'], },
      credentials: { api_key: 'test', model_mapping: { alias: 'target' } }
    })
    expect(form.credentials.model_mapping).toHaveProperty('allowed')
    expect(form).not.toHaveProperty('model_catalog_policy')
  })

  it('applies the same policy conversion to batch creation', async () => {
    await batchCreate([input(), input({ credentials: {} })])
    const payload = post.mock.calls[0][1].accounts
    expect(payload[0].model_catalog_policy.models).toEqual(['allowed', 'alias'])
    expect(payload[1].model_catalog_policy).toEqual({ models: [], })
  })

  it.each([
    input({ platform: 'antigravity' }),
    input({ extra: { openai_passthrough: true } }),
    input({ type: 'oauth', credentials: { model_mapping_mode: 'aliases', model_mapping: { alias: 'target' } } }),
    input({ model_catalog_policy: { models: [], } }),
    input({ credentials: { model_mapping: {} } })
  ])('preserves routing and uses an unrestricted empty selection: %j', async form => {
    await create(form)
    expect(post).toHaveBeenCalledWith('/admin/accounts', {
      ...form, model_catalog_policy: form.model_catalog_policy ?? { models: [], }
    })
  })

  it.each(['grok', 'gemini'] as const)('preserves explicit identity routing on %s', async platform => {
    await create(input({ platform }))
    expect(post.mock.calls[0][1].credentials.model_mapping).toEqual({ allowed: 'allowed', alias: 'target' })
    expect(post.mock.calls[0][1].model_catalog_policy.models).toEqual(['allowed', 'alias'])
  })
})
