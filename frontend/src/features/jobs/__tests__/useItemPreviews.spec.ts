import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BatchImageItem } from '../api/batchImage'
import { useItemPreviews } from '../queries/useItemPreviews'

const cache = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), remove: vi.fn(), cleanup: vi.fn(), dispose: vi.fn() }))
const content = vi.hoisted(() => vi.fn())
const thumbnail = vi.hoisted(() => vi.fn())
vi.mock('../model/previewCache', async importOriginal => ({
  ...await importOriginal<typeof import('../model/previewCache')>(),
  createPreviewCache: () => cache,
}))
vi.mock('../model/previewThumbnail', () => ({ createPreviewThumbnail: thumbnail }))
vi.mock('../api/batchImage', () => ({ getBatchImageItemContent: content }))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Promise not initialized') }
  let reject: (error: Error) => void = () => { throw new Error('Promise not initialized') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const item: BatchImageItem = { custom_id: 'same:id', batch_id: 'parent', status: 'succeeded', image_count: 1, mime_type: 'image/png', file_extension: 'png' }
const original = new Blob(['original'])
const small = new Blob(['thumbnail'])
const wrappers: ReturnType<typeof mount>[] = []
function setup() {
  const scopeKey = ref<string | null>('session-a:user:personal')
  const apiKeyId = ref(7)
  const batchId = ref('parent')
  const onError = vi.fn()
  let owner: ReturnType<typeof useItemPreviews> | undefined
  const wrapper = mount(defineComponent({
    setup() {
      owner = useItemPreviews({ scopeKey, apiKeyId, batchId, readApiKey: () => 'secret-api-key', onError })
      return () => null
    },
  }))
  wrappers.push(wrapper)
  if (!owner) throw new Error('Owner not mounted')
  return { owner, wrapper, scopeKey, apiKeyId, batchId, onError }
}
beforeEach(() => {
  vi.resetAllMocks()
  cache.get.mockResolvedValue(null)
  cache.put.mockResolvedValue(undefined)
  cache.remove.mockResolvedValue(undefined)
  cache.cleanup.mockResolvedValue(undefined)
  content.mockResolvedValue(original)
  thumbnail.mockResolvedValue(small)
  let serial = 0
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => `blob:preview-${++serial}`), revokeObjectURL: vi.fn() })
})
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('item preview ownership', () => {
  it('consumes cached thumbnails, revokes displayed URLs, closes dialog and stops cleanup on leave', async () => {
    vi.useFakeTimers()
    cache.get.mockResolvedValue(small)
    const { owner, wrapper } = setup()
    await owner.loadItemPreview(item)
    const key = owner.itemPreviewKey(item)
    expect(key).toBe('session-a%3Auser%3Apersonal:7:parent:same%3Aid:0')
    expect(cache.get).toHaveBeenCalledWith(key, expect.any(AbortSignal))
    expect(content).not.toHaveBeenCalled()
    owner.openImagePreview(item)
    expect(owner.previewImageUrl.value).toBe('blob:preview-1')
    expect(owner.previewLoadingIds.value.size).toBe(0)
    wrapper.unmount()
    expect(owner.previewImageUrl.value).toBe('')
    expect(owner.previewImageItem.value).toBeNull()
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(1)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
    expect(cache.dispose).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(60 * 60 * 1000)
    expect(cache.cleanup).toHaveBeenCalledTimes(1)
  })

  it.each(['clear', 'detail', 'actor', 'key', 'logout', 'leave'] as const)('rejects a late content response after %s', async transition => {
    const pending = deferred<Blob>()
    content.mockReturnValue(pending.promise)
    const state = setup()
    const work = state.owner.loadItemPreview(item)
    await flushPromises()
    const signal: AbortSignal = content.mock.calls[0][4]
    if (transition === 'clear') state.owner.clearItemPreviews()
    if (transition === 'detail') state.batchId.value = 'another-detail'
    if (transition === 'actor') state.scopeKey.value = 'session-b:user:personal'
    if (transition === 'key') state.apiKeyId.value = 8
    if (transition === 'logout') state.scopeKey.value = null
    if (transition === 'leave') state.wrapper.unmount()
    expect(signal.aborted).toBe(true)
    expect(state.owner.previewLoadingIds.value.size).toBe(0)
    pending.resolve(original)
    await work
    expect(thumbnail).not.toHaveBeenCalled()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(cache.put).not.toHaveBeenCalled()
    expect(state.onError).not.toHaveBeenCalled()
  })

  it('rejects late decoding and aborts pending cache writes when the detail is cleared', async () => {
    const decoded = deferred<Blob>()
    thumbnail.mockReturnValueOnce(decoded.promise)
    const { owner, onError } = setup()
    const work = owner.loadItemPreview(item)
    await flushPromises()
    const signal: AbortSignal = thumbnail.mock.calls[0][1]
    owner.clearItemPreviews()
    decoded.resolve(small)
    await work
    expect(signal.aborted).toBe(true)
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(cache.put).not.toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()

    const writing = deferred<void>()
    cache.put.mockReturnValueOnce(writing.promise)
    thumbnail.mockResolvedValue(small)
    const next = owner.loadItemPreview(item)
    await flushPromises()
    const writeSignal: AbortSignal = cache.put.mock.calls[0][2]
    owner.clearItemPreviews()
    expect(writeSignal.aborted).toBe(true)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
    writing.resolve()
    await next
    expect(owner.itemPreviewUrls).toEqual({})
  })

  it('keeps identical custom IDs in parent and child batches independent', async () => {
    const first = deferred<Blob>()
    content.mockReturnValueOnce(first.promise)
    const { owner } = setup()
    const a = owner.loadItemPreview(item)
    await flushPromises()
    const signal: AbortSignal = content.mock.calls[0][4]
    const child = { ...item, batch_id: 'child' }
    await owner.loadItemPreview(child)
    expect(signal.aborted).toBe(false)
    first.resolve(original)
    await a
    expect(Object.keys(owner.itemPreviewUrls)).toEqual(expect.arrayContaining([owner.itemPreviewKey(item), owner.itemPreviewKey(child)]))
    expect(owner.previewLoadingIds.value.size).toBe(0)
    expect(cache.put).toHaveBeenCalledTimes(2)
  })

  it('does not let a superseded click clear the new request loading state or report an old failure', async () => {
    const first = deferred<Blob>()
    const second = deferred<Blob>()
    content.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { owner, onError } = setup()
    const a = owner.loadItemPreview(item)
    await flushPromises()
    const b = owner.loadItemPreview(item)
    await flushPromises()
    first.reject(new Error('old failure'))
    await a
    expect(owner.previewLoadingIds.value.has(owner.itemPreviewKey(item))).toBe(true)
    expect(onError).not.toHaveBeenCalled()
    second.resolve(original)
    await b
    expect(owner.previewLoadingIds.value.size).toBe(0)
  })

  it('supersedes late hydration on click, and does not let hydration interrupt an active click', async () => {
    const cached = deferred<Blob | null>()
    cache.get.mockReturnValueOnce(cached.promise)
    const { owner } = setup()
    const hydration = owner.hydrateCachedItemPreviews([item])
    await owner.loadItemPreview(item)
    cached.resolve(small)
    await hydration
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)

    owner.clearItemPreviews()
    const pending = deferred<Blob>()
    content.mockReturnValueOnce(pending.promise)
    const work = owner.loadItemPreview(item)
    await flushPromises()
    const signal: AbortSignal = content.mock.calls[1][4]
    await owner.hydrateCachedItemPreviews([item])
    expect(signal.aborted).toBe(false)
    pending.resolve(original)
    await work
  })

  it('discards a late cache hit after scope change without starting a content request', async () => {
    const cached = deferred<Blob | null>()
    cache.get.mockReturnValueOnce(cached.promise)
    const { owner, scopeKey } = setup()
    const work = owner.hydrateCachedItemPreviews([item])
    const signal: AbortSignal = cache.get.mock.calls[0][1]
    scopeKey.value = 'new-actor'
    cached.resolve(small)
    await work
    expect(signal.aborted).toBe(true)
    expect(owner.itemPreviewUrls).toEqual({})
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(content).not.toHaveBeenCalled()
  })

  it.each(['hydrate', 'click'] as const)('rechecks scope at publication after the %s cache wait has resolved', async action => {
    const cached = deferred<Blob | null>()
    cache.get.mockReturnValueOnce(cached.promise)
    const { owner, scopeKey, onError } = setup()
    const work = action === 'hydrate' ? owner.hydrateCachedItemPreviews([item]) : owner.loadItemPreview(item)
    // Runs after useJobsRequest.wait checks ownership, but before its caller resumes.
    const changed = cached.promise.then(() => { scopeKey.value = 'session-b:user:personal' })
    cached.resolve(small)
    await Promise.all([work, changed])
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(owner.itemPreviewUrls).toEqual({})
    expect(owner.previewLoadingIds.value.size).toBe(0)
    expect(content).not.toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()
  })

  it('does not send old credentials when the scope changes just after a cache miss', async () => {
    const cached = deferred<Blob | null>()
    cache.get.mockReturnValueOnce(cached.promise)
    const { owner, scopeKey } = setup()
    const work = owner.loadItemPreview(item)
    const changed = cached.promise.then(() => { scopeKey.value = null })
    cached.resolve(null)
    await Promise.all([work, changed])
    expect(content).not.toHaveBeenCalled()
  })

  it('releases a broken displayed URL and allows an explicit reload', async () => {
    const { owner, onError } = setup()
    cache.get.mockResolvedValue(small)
    await owner.loadItemPreview(item)
    const key = owner.itemPreviewKey(item)
    owner.openImagePreview(item)
    owner.handlePreviewError(key, 'blob:preview-1')
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
    expect(owner.previewErrorIds.value.has(key)).toBe(true)
    expect(owner.previewImageItem.value).toBeNull()
    expect(cache.remove).toHaveBeenCalledWith(key, expect.any(AbortSignal))
    await owner.loadItemPreview(item)
    expect(cache.get).toHaveBeenCalledTimes(1)
    expect(content).toHaveBeenCalledTimes(1)
    expect(owner.previewErrorIds.value.has(key)).toBe(false)
    expect(owner.itemPreviewUrls[key]).toBe('blob:preview-2')
    expect(onError).not.toHaveBeenCalled()
  })

  it('ignores a stale image error after a reload, a scope switch and unmount', async () => {
    const { owner, scopeKey, wrapper } = setup()
    await owner.loadItemPreview(item)
    const key = owner.itemPreviewKey(item)
    owner.handlePreviewError(key, 'blob:preview-1')
    await owner.loadItemPreview(item)
    owner.handlePreviewError(key, 'blob:preview-1')
    expect(owner.itemPreviewUrls[key]).toBe('blob:preview-2')
    expect(owner.previewErrorIds.value.size).toBe(0)
    scopeKey.value = 'session-b:user:personal'
    const nextKey = owner.itemPreviewKey(item)
    await owner.loadItemPreview(item)
    owner.handlePreviewError(nextKey, 'blob:preview-2')
    expect(owner.itemPreviewUrls[nextKey]).toBe('blob:preview-3')
    wrapper.unmount()
    owner.handlePreviewError(nextKey, 'blob:preview-3')
    expect(owner.previewErrorIds.value.size).toBe(0)
    expect(cache.remove).toHaveBeenCalledTimes(1)
  })

  it.each([1024 * 1024, 1024 * 1024 + 1])('retains decoding fallback and the original-image persistence size limit: %i bytes', async size => {
    const blob = new Blob([new Uint8Array(size)])
    content.mockResolvedValue(blob)
    thumbnail.mockRejectedValue(new Error('unsupported decoder'))
    const { owner } = setup()
    await owner.loadItemPreview(item)
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob)
    expect(cache.put).toHaveBeenCalledTimes(size <= 1024 * 1024 ? 1 : 0)
  })

  it('reports an active API error and releases loading without caching it', async () => {
    const error = new Error('preview unavailable')
    content.mockRejectedValue(error)
    const { owner, onError } = setup()
    await owner.loadItemPreview(item)
    expect(onError).toHaveBeenCalledWith(error)
    expect(owner.previewLoadingIds.value.size).toBe(0)
    expect(owner.previewErrorIds.value.has(owner.itemPreviewKey(item))).toBe(true)
    expect(cache.put).not.toHaveBeenCalled()
  })
})
