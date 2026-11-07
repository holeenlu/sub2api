import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, defineComponent, effectScope } from 'vue'
import { mount } from '@vue/test-utils'
import { uiFeedbackKey } from '@/shared/ui/feedback'

// Mock i18n
vi.mock('@/i18n', () => ({
  i18n: {
    global: {
      t: (key: string) => key,
    },
  },
}))

// Explicit app-owned feedback callbacks; this helper does not require Pinia.
const mockShowSuccess = vi.fn()
const mockShowError = vi.fn()

import { useClipboard } from '@/shared/lib/useClipboard'

function createClipboard() {
  const app = createApp({ render: () => null })
  app.provide(uiFeedbackKey, { success: mockShowSuccess, error: mockShowError })
  return app.runWithContext(() => useClipboard())
}

describe('useClipboard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()

    // 默认模拟安全上下文 + Clipboard API
    Object.defineProperty(window, 'isSecureContext', { value: true, writable: true })
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    // 恢复 execCommand
    if ('execCommand' in document) {
      delete (document as any).execCommand
    }
  })

  it('复制成功后 copied 变为 true', async () => {
    const { copied, copyToClipboard } = createClipboard()

    expect(copied.value).toBe(false)

    await copyToClipboard('hello')

    expect(copied.value).toBe(true)
  })

  it('copied 在 2 秒后自动恢复为 false', async () => {
    const { copied, copyToClipboard } = createClipboard()

    await copyToClipboard('hello')
    expect(copied.value).toBe(true)

    vi.advanceTimersByTime(2000)

    expect(copied.value).toBe(false)
  })

  it('复制成功时调用 showSuccess', async () => {
    const { copyToClipboard } = createClipboard()

    await copyToClipboard('hello', '已复制')

    expect(mockShowSuccess).toHaveBeenCalledWith('已复制')
  })

  it('无自定义消息时使用 i18n 默认消息', async () => {
    const { copyToClipboard } = createClipboard()

    await copyToClipboard('hello')

    expect(mockShowSuccess).toHaveBeenCalledWith('common.copiedToClipboard')
  })

  it('空文本返回 false 且不复制', async () => {
    const { copyToClipboard, copied } = createClipboard()

    const result = await copyToClipboard('')

    expect(result).toBe(false)
    expect(copied.value).toBe(false)
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
    expect(mockShowSuccess).not.toHaveBeenCalled()
    expect(mockShowError).not.toHaveBeenCalled()
  })

  it('Clipboard API 失败时降级到 fallback', async () => {
    const writeTextMock = navigator.clipboard.writeText as any
    writeTextMock.mockRejectedValue(new Error('API failed'))

    // jsdom 没有 execCommand，手动定义
    const documentAny = document as any
    documentAny.execCommand = vi.fn().mockReturnValue(true)

    const { copyToClipboard, copied } = createClipboard()
    const result = await copyToClipboard('fallback text')

    expect(result).toBe(true)
    expect(copied.value).toBe(true)
    expect(document.execCommand).toHaveBeenCalledWith('copy')
  })

  it('非安全上下文使用 fallback', async () => {
    Object.defineProperty(window, 'isSecureContext', { value: false, writable: true })

    const documentAny = document as any
    documentAny.execCommand = vi.fn().mockReturnValue(true)

    const { copyToClipboard, copied } = createClipboard()
    const result = await copyToClipboard('insecure context text')

    expect(result).toBe(true)
    expect(copied.value).toBe(true)
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
    expect(document.execCommand).toHaveBeenCalledWith('copy')
  })

  it.each([false, true])('reports a thrown fallback error (secure context: %s)', async (secure) => {
    Object.defineProperty(window, 'isSecureContext', { value: secure, writable: true })
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('Clipboard denied'))
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: vi.fn(() => { throw new DOMException('Copy denied', 'NotAllowedError') })
    })
    const { copyToClipboard, copied } = createClipboard()
    await expect(copyToClipboard('text')).resolves.toBe(false)
    expect(copied.value).toBe(false)
    expect(mockShowError).toHaveBeenCalledWith('common.copyFailed')
    expect(mockShowSuccess).not.toHaveBeenCalled()
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('所有复制方式均失败时调用 showError', async () => {
    const writeTextMock = navigator.clipboard.writeText as any
    writeTextMock.mockRejectedValue(new Error('fail'))

    const documentAny = document as any
    documentAny.execCommand = vi.fn().mockReturnValue(false)

    const { copyToClipboard, copied } = createClipboard()
    const result = await copyToClipboard('text')

    expect(result).toBe(false)
    expect(copied.value).toBe(false)
    expect(mockShowError).toHaveBeenCalled()
  })
})


describe('clipboard compatibility failures', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(window, 'isSecureContext', { value: false, writable: true })
  })
  afterEach(() => vi.restoreAllMocks())

  it('cleans up and reports selection errors through the provided feedback', async () => {
    vi.spyOn(HTMLTextAreaElement.prototype, 'select').mockImplementationOnce(() => {
      throw new DOMException('Selection unavailable', 'NotSupportedError')
    })
    const { copyToClipboard, copied } = createClipboard()
    await expect(copyToClipboard('first line\nsecond line')).resolves.toBe(false)
    expect(copied.value).toBe(false)
    expect(mockShowError).toHaveBeenCalledWith('common.copyFailed')
    expect(mockShowSuccess).not.toHaveBeenCalled()
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('reports failure when neither native clipboard nor execCommand exists', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true, writable: true })
    const { copyToClipboard } = createClipboard()
    await expect(copyToClipboard('text')).resolves.toBe(false)
    expect(mockShowError).toHaveBeenCalledWith('common.copyFailed')
    expect(document.querySelector('textarea')).toBeNull()
  })
})

describe('clipboard owner lifecycle', () => {
  const cleanups: Array<() => void> = []

  function createOwner(kind: 'host' | 'component') {
    let clipboard!: ReturnType<typeof useClipboard>
    let stop: () => void
    if (kind === 'host') {
      const scope = effectScope()
      // Mirrors the installer: active effect scope plus application injection context.
      clipboard = scope.run(createClipboard)!
      stop = () => scope.stop()
    } else {
      const wrapper = mount(defineComponent({
        setup() {
          clipboard = useClipboard()
          return () => null
        },
      }), { global: { provide: { [uiFeedbackKey as symbol]: { success: mockShowSuccess, error: mockShowError } } } })
      stop = () => wrapper.unmount()
    }
    let stopped = false
    const dispose = () => {
      if (stopped) return
      stopped = true
      stop()
    }
    cleanups.push(dispose)
    return { ...clipboard, dispose }
  }

  beforeEach(() => {
    vi.useFakeTimers()
    mockShowSuccess.mockReset()
    mockShowError.mockReset()
    Object.defineProperty(window, 'isSecureContext', { value: true, writable: true })
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    for (const dispose of cleanups.splice(0)) dispose()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    if ('execCommand' in document) delete (document as any).execCommand
  })

  it.each(['host', 'component'] as const)('clears copied state and its reset timer when the %s stops', async kind => {
    const clipboard = createOwner(kind)
    await expect(clipboard.copyToClipboard('text')).resolves.toBe(true)
    expect(clipboard.copied.value).toBe(true)
    expect(vi.getTimerCount()).toBe(1)

    clipboard.dispose()
    expect(clipboard.copied.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    vi.advanceTimersByTime(2000)
    expect(vi.getTimerCount()).toBe(0)
    expect(mockShowSuccess).toHaveBeenCalledOnce()
  })

  describe.each(['host', 'component'] as const)('late %s completion', kind => {
    it.each([
      { nativeSuccess: true, fallbackSuccess: false, expected: true },
      { nativeSuccess: false, fallbackSuccess: true, expected: true },
      { nativeSuccess: false, fallbackSuccess: false, expected: false },
    ])('returns the copy result without feedback: $nativeSuccess / $fallbackSuccess', async ({ nativeSuccess, fallbackSuccess, expected }) => {
      let resolve!: () => void
      let reject!: (error: Error) => void
      const pending = new Promise<void>((yes, no) => { resolve = yes; reject = no })
      vi.mocked(navigator.clipboard.writeText).mockReturnValueOnce(pending)
      Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn().mockReturnValue(fallbackSuccess) })
      const clipboard = createOwner(kind)
      const result = clipboard.copyToClipboard('pending text')
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('pending text')

      clipboard.dispose()
      expect(vi.getTimerCount()).toBe(0)
      if (nativeSuccess) resolve()
      else reject(new Error('Clipboard denied'))

      await expect(result).resolves.toBe(expected)
      expect(document.execCommand).toHaveBeenCalledTimes(nativeSuccess ? 0 : 1)
      expect(clipboard.copied.value).toBe(false)
      expect(mockShowSuccess).not.toHaveBeenCalled()
      expect(mockShowError).not.toHaveBeenCalled()
      // jsdom queues 0ms textarea select events during the real fallback copy.
      // Drain those only; any leaked 2s copied-reset timer remains observable.
      await vi.advanceTimersByTimeAsync(0)
      expect(vi.getTimerCount()).toBe(0)
      expect(document.querySelector('textarea')).toBeNull()
    })
  })

  it('keeps one reset timer measured from the latest successful copy', async () => {
    const clipboard = createOwner('host')
    await clipboard.copyToClipboard('first')
    vi.advanceTimersByTime(1500)
    await clipboard.copyToClipboard('second')
    expect(vi.getTimerCount()).toBe(1)
    vi.advanceTimersByTime(500)
    expect(clipboard.copied.value).toBe(true)
    vi.advanceTimersByTime(1500)
    expect(clipboard.copied.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not leave a timer if the success feedback disposes its owner', async () => {
    const clipboard = createOwner('host')
    mockShowSuccess.mockImplementationOnce(() => clipboard.dispose())
    await expect(clipboard.copyToClipboard('text')).resolves.toBe(true)
    expect(clipboard.copied.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('preserves an explicitly invoked copy after disposal without reviving UI state', async () => {
    const clipboard = createOwner('host')
    clipboard.dispose()
    await expect(clipboard.copyToClipboard('explicit copy')).resolves.toBe(true)
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('explicit copy')
    expect(clipboard.copied.value).toBe(false)
    expect(mockShowSuccess).not.toHaveBeenCalled()
    expect(mockShowError).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
