import { beforeEach, describe, expect, it, vi } from 'vitest'
import { managedModelsAPI } from '../api/managedModels'
import type { ModelPolicy } from '../api/managedModelTypes'
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: client }))
beforeEach(() => {
  vi.resetAllMocks()
  client.get.mockResolvedValue({ data: [] })
  client.put.mockResolvedValue({ data: { version: 8 } })
  client.post.mockResolvedValue({ data: { version: 8 } })
})
describe('managed catalog wire contract', () => {
  it('keeps policy and mapping versions separate and carries cancellation to each request', async () => {
    const signal = new AbortController().signal
    await managedModelsAPI.policy('anthropic', { signal })
    await managedModelsAPI.mappings({ signal })
    expect(client.get.mock.calls).toEqual([
      ['/admin/models/policies/anthropic', { signal }],
      ['/admin/models/mappings', { signal }]
    ])
    const mapping = { version: 13, mappings: { gpt: 'gpt-example' } }
    await managedModelsAPI.saveMappings(mapping, { signal })
    expect(client.put).toHaveBeenCalledWith('/admin/models/mappings', mapping, { signal })
  })
  it('uses source IDs only for catalog synchronization with its explicit timeout', async () => {
    const signal = new AbortController().signal
    await managedModelsAPI.sources('openai', { signal })
    await managedModelsAPI.catalog('openai', { signal })
    await managedModelsAPI.sync('openai', 7, [4, 6], { signal })
    expect(client.get.mock.calls).toEqual([
      ['/admin/models/sources', { signal, params: { platform: 'openai' } }],
      ['/admin/models/catalog', { signal, params: { platform: 'openai' } }]
    ])
    expect(client.post).toHaveBeenCalledWith('/admin/models/policies/openai/sync', { version: 7, account_ids: [4, 6] }, { signal, timeout: 65000 })
  })
  it('posts reset with the current policy version', async () => {
    await managedModelsAPI.reset('anthropic', 42)
    expect(client.post).toHaveBeenCalledWith('/admin/models/policies/anthropic/reset', { version: 42 }, {})
  })
  it('preserves advanced policy fields, aliases and beta inheritance without normalization', async () => {
    const value: ModelPolicy = { version: 8, platform: 'anthropic', source: 'builtin', catalog_mode: 'policy_only', models: {}, aliases: { sonnet: 'example' }, defaults: { enabled: false, max_tokens: 8192, thinking_fallback_budget: 1024, strip_context_1m: false, context_1m_whitelist: ['example'], normalize_thinking: true } }
    await managedModelsAPI.save(value)
    expect(client.put).toHaveBeenCalledWith('/admin/models/policies/anthropic', value, {})
  })
})
