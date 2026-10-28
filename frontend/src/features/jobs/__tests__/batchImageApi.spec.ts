import { afterEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/batchImage'
vi.mock('@/shared/http', () => ({ buildGatewayUrl: (path: string) => 'https://gateway.test' + path }))
afterEach(() => vi.unstubAllGlobals())
describe('batch image API credential and wire contract', () => {
  it('preserves every submit field and sends only the explicit generation key', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'batch-1', actual_cost: null }) })
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('auth_token', 'admin-jwt-never-send')
    const payload: api.BatchImageSubmitRequest = {
      model: 'image-model', task_name: 'task', parent_batch_id: 'parent', provider: 'vertex', image_size: '4K',
      response_mime_type: 'image/jpeg', aspect_ratio: '16:9', metadata: { caller: 'test' },
      items: [{
        custom_id: 'one', prompt: 'scene', output_count: 2, reference_images: [
          { id: 'ref', type: 'subject', mime_type: 'image/png', data: 'base64', file_uri: 'file:test' },
        ]
      }],
    }
    const signal = new AbortController().signal
    const result = await api.submitBatchImageJob('generation-key', payload, 'request-once', signal)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('https://gateway.test/v1/images/batches')
    expect(JSON.parse(init.body)).toEqual(payload)
    expect(init).toMatchObject({
      signal, credentials: 'omit', redirect: 'error', headers: {
        Authorization: 'Bearer generation-key', 'Idempotency-Key': 'request-once',
      }
    })
    expect(JSON.stringify(init)).not.toContain('admin-jwt-never-send')
    expect(result.actual_cost).toBeNull()
    localStorage.removeItem('auth_token')
  })
  it('preserves a server still-running cancellation result and propagates transport abort', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'a/b', status: 'running', actual_cost: null }) })
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    expect(await api.cancelBatchImageJob('key', 'a/b', controller.signal)).toEqual({ id: 'a/b', status: 'running', actual_cost: null })
    expect(fetchMock.mock.calls[0][0]).toContain('/a%2Fb/cancel')
    expect(fetchMock.mock.calls[0][1].signal).toBe(controller.signal)
    fetchMock.mockRejectedValueOnce(new DOMException('Stopped waiting', 'AbortError'))
    await expect(api.cancelBatchImageJob('key', 'a/b', controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
  it('preserves all list filters and artifact coordinates', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: [], has_more: false }), blob: async () => new Blob(['image']) })
    vi.stubGlobal('fetch', fetchMock)
    await api.listBatchImageJobs('key', { limit: 10, cursor: '20', status: 'running', taskName: 'name', downloaded: 'false', from: 'start', to: 'end' })
    const url = new URL(fetchMock.mock.calls[0][0])
    expect(Object.fromEntries(url.searchParams)).toEqual({ limit: '10', cursor: '20', status: 'running', task_name: 'name', downloaded: 'false', from: 'start', to: 'end' })
    await api.getBatchImageItemContent('key', 'batch/id', 'item/id', 3)
    expect(fetchMock.mock.calls[1][0]).toContain('/batch%2Fid/items/item%2Fid/content?image_index=3')
  })
})
