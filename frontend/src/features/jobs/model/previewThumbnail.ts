const MAX_EDGE = 360
const QUALITY = 0.72

interface ImageSource {
  image: ImageBitmap | HTMLImageElement
  width: number
  height: number
  close(): void
}

function check(signal: AbortSignal): void {
  if (signal.aborted) throw new DOMException('Preview cancelled', 'AbortError')
}

async function loadImage(blob: Blob, signal: AbortSignal): Promise<ImageSource> {
  check(signal)
  if (typeof window.createImageBitmap === 'function') {
    // Decoding cannot be aborted, but the owner can stop waiting. Close late bitmaps.
    return new Promise<ImageSource>((resolve, reject) => {
      const abort = () => reject(new DOMException('Preview cancelled', 'AbortError'))
      signal.addEventListener('abort', abort, { once: true })
      try {
        void window.createImageBitmap(blob).then(bitmap => {
          signal.removeEventListener('abort', abort)
          if (signal.aborted) { bitmap.close(); abort(); return }
          resolve({ image: bitmap, width: bitmap.width, height: bitmap.height, close: () => bitmap.close() })
        }, error => {
          signal.removeEventListener('abort', abort)
          reject(error)
        })
      } catch (error) {
        signal.removeEventListener('abort', abort)
        reject(error)
      }
    })
  }

  const url = URL.createObjectURL(blob)
  const image = new Image()
  const close = () => {
    image.onload = null
    image.onerror = null
    image.removeAttribute('src')
    URL.revokeObjectURL(url)
  }
  try {
    await new Promise<void>((resolve, reject) => {
      const finish = (error?: Error) => {
        signal.removeEventListener('abort', abort)
        image.onload = null
        image.onerror = null
        if (error) reject(error)
        else resolve()
      }
      const abort = () => finish(new DOMException('Preview cancelled', 'AbortError'))
      image.onload = () => finish()
      image.onerror = () => finish(new Error('image unavailable'))
      signal.addEventListener('abort', abort, { once: true })
      image.src = url
    })
    check(signal)
    return { image, width: image.naturalWidth || image.width, height: image.naturalHeight || image.height, close }
  } catch (error) {
    close()
    throw error
  }
}

export async function createPreviewThumbnail(blob: Blob, signal: AbortSignal): Promise<Blob> {
  const source = await loadImage(blob, signal)
  let canvas: HTMLCanvasElement | undefined
  try {
    try {
      check(signal)
      const scale = Math.min(1, MAX_EDGE / Math.max(source.width, source.height))
      canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(source.width * scale))
      canvas.height = Math.max(1, Math.round(source.height * scale))
      const context = canvas.getContext('2d')
      if (!context) throw new Error('canvas unavailable')
      context.drawImage(source.image, 0, 0, canvas.width, canvas.height)
    } finally {
      // Release both bitmap and fallback image URLs even if canvas setup/drawing fails.
      source.close()
    }
    check(signal)
    const output = canvas
    return await new Promise<Blob>((resolve, reject) => {
      const abort = () => reject(new DOMException('Preview cancelled', 'AbortError'))
      signal.addEventListener('abort', abort, { once: true })
      try {
        output.toBlob(thumbnail => {
          signal.removeEventListener('abort', abort)
          if (signal.aborted) { abort(); return }
          if (thumbnail) resolve(thumbnail)
          else reject(new Error('thumbnail unavailable'))
        }, 'image/webp', QUALITY)
      } catch (error) {
        signal.removeEventListener('abort', abort)
        reject(error)
      }
    })
  } finally {
    // Do not retain the canvas backing store while an obsolete callback is outstanding.
    if (canvas) { canvas.width = 0; canvas.height = 0 }
  }
}
