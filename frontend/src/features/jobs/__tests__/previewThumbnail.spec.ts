import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPreviewThumbnail } from '../model/previewThumbnail'

const blob = new Blob(['source'])
const thumbnail = new Blob(['webp'])
function bitmap() { return { width: 720, height: 360, close: vi.fn() } }
function canvas() {
  const element = document.createElement('canvas')
  const drawImage = vi.fn()
  Object.defineProperty(element, 'getContext', { configurable: true, value: vi.fn(() => ({ drawImage })) })
  const encode = vi.spyOn(element, 'toBlob').mockImplementation(callback => callback(thumbnail))
  vi.spyOn(document, 'createElement').mockReturnValue(element)
  return { element, drawImage, encode }
}
beforeEach(() => {
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:decode'), revokeObjectURL: vi.fn() })
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('preview decoder resources', () => {
  it('keeps 360px/WebP/0.72 output, closes the bitmap and releases the canvas backing store', async () => {
    const source = bitmap()
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue(source))
    const { element, drawImage, encode } = canvas()
    encode.mockImplementation((callback, type, quality) => {
      expect([element.width, element.height, type, quality]).toEqual([360, 180, 'image/webp', 0.72])
      expect(source.close).toHaveBeenCalledTimes(1)
      callback(thumbnail)
    })
    expect(await createPreviewThumbnail(blob, new AbortController().signal)).toBe(thumbnail)
    expect(drawImage).toHaveBeenCalledWith(source, 0, 0, 360, 180)
    expect(source.close).toHaveBeenCalledTimes(1)
    expect([element.width, element.height]).toEqual([0, 0])
  })

  it.each(['context', 'draw', 'encode', 'empty'] as const)('closes the bitmap on %s failure', async failure => {
    const source = bitmap()
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue(source))
    const { element, drawImage, encode } = canvas()
    if (failure === 'context') Object.defineProperty(element, 'getContext', { value: () => null })
    if (failure === 'draw') drawImage.mockImplementation(() => { throw new Error('draw failed') })
    if (failure === 'encode') encode.mockImplementation(() => { throw new Error('encode failed') })
    if (failure === 'empty') encode.mockImplementation(callback => callback(null))
    await expect(createPreviewThumbnail(blob, new AbortController().signal)).rejects.toThrow()
    expect(source.close).toHaveBeenCalledTimes(1)
    expect([element.width, element.height]).toEqual([0, 0])
  })

  it('closes a bitmap that resolves after cancellation without allocating a canvas', async () => {
    const source = bitmap()
    let complete: (value: ReturnType<typeof bitmap>) => void = () => { throw new Error('Decoder not started') }
    vi.stubGlobal('createImageBitmap', vi.fn(() => new Promise(resolve => { complete = resolve })))
    const create = vi.spyOn(document, 'createElement')
    const controller = new AbortController()
    const work = createPreviewThumbnail(blob, controller.signal)
    const cancelled = vi.fn()
    const result = work.catch(cancelled)
    controller.abort()
    await flushPromises()
    const settledBeforeDecode = cancelled.mock.calls.length
    complete(source)
    await result
    expect(settledBeforeDecode).toBe(1)
    expect(cancelled).toHaveBeenCalledWith(expect.objectContaining({ name: 'AbortError' }))
    await expect(work).rejects.toMatchObject({ name: 'AbortError' })
    expect(source.close).toHaveBeenCalledTimes(1)
    expect(create).not.toHaveBeenCalled()
  })

  it('rejects cancelled encoding immediately and ignores a late toBlob callback', async () => {
    const source = bitmap()
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue(source))
    const { element, encode } = canvas()
    let complete: BlobCallback = () => { throw new Error('Encoding not started') }
    encode.mockImplementation(callback => { complete = callback })
    const controller = new AbortController()
    const work = createPreviewThumbnail(blob, controller.signal)
    await flushPromises()
    controller.abort()
    await expect(work).rejects.toMatchObject({ name: 'AbortError' })
    expect(source.close).toHaveBeenCalledTimes(1)
    expect([element.width, element.height]).toEqual([0, 0])
    complete(thumbnail)
    await expect(work).rejects.toMatchObject({ name: 'AbortError' })
  })

  it.each(['load', 'error', 'abort'] as const)('releases fallback Image URL, handlers and src after %s', async outcome => {
    vi.stubGlobal('createImageBitmap', undefined)
    const image = document.createElement('img')
    image.width = 100
    image.height = 50
    vi.stubGlobal('Image', vi.fn(function () { return image }))
    const { drawImage } = canvas()
    const controller = new AbortController()
    const work = createPreviewThumbnail(blob, controller.signal)
    expect(image.getAttribute('src')).toBe('blob:decode')
    if (outcome === 'abort') controller.abort()
    else image.dispatchEvent(new Event(outcome))
    if (outcome === 'load') {
      expect(await work).toBe(thumbnail)
      expect(drawImage).toHaveBeenCalledWith(image, 0, 0, 100, 50)
    } else {
      await expect(work).rejects.toMatchObject({ name: outcome === 'abort' ? 'AbortError' : 'Error' })
    }
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(1)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:decode')
    expect(image.onload).toBeNull()
    expect(image.onerror).toBeNull()
    expect(image.getAttribute('src')).toBeNull()
  })

  it('does not start decoding for an already-cancelled request', async () => {
    const decode = vi.fn()
    vi.stubGlobal('createImageBitmap', decode)
    const controller = new AbortController()
    controller.abort()
    await expect(createPreviewThumbnail(blob, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(decode).not.toHaveBeenCalled()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })
})
