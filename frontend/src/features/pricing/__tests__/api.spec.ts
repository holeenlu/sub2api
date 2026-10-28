import { beforeEach, describe, expect, it, vi } from 'vitest'
import channelsAPI from '../api/channels'
const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { get, post, put, delete: remove } }))
beforeEach(() => { vi.clearAllMocks(); post.mockResolvedValue({ data: { id: 7 } }); put.mockResolvedValue({ data: { id: 7 } }) })
describe('channel REST contract', () => {
 it('submits the whole channel draft once without synthesizing revision', async () => {
  const draft = { name: 'rate', group_ids: [3], model_pricing: [], features_config: { retained: false } }
  await channelsAPI.create(draft)
  expect(post).toHaveBeenCalledTimes(1)
  expect(post).toHaveBeenCalledWith('/admin/channels', draft)
  expect(put).not.toHaveBeenCalled(); expect(get).not.toHaveBeenCalled()
 })
 it('retains PUT update and signal-aware price reads', async () => {
  const signal = new AbortController().signal; get.mockResolvedValue({ data: { found: false } })
  const patch = { restrict_models: false }
  await channelsAPI.update(7, patch)
  expect(put).toHaveBeenCalledTimes(1)
  expect(put).toHaveBeenCalledWith('/admin/channels/7', patch)
  expect(await channelsAPI.getModelDefaultPricing('unknown-model', signal)).toEqual({ found: false })
  expect(get).toHaveBeenCalledWith('/admin/channels/model-pricing', { params: { model: 'unknown-model' }, signal })
 })
})
