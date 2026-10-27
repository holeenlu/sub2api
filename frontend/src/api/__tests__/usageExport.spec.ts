import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import { consumeUsageExport, escapeUsageCSV } from '@/api/usageExport'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn() } }))
const get = vi.mocked(apiClient.get)
const encoder = new TextEncoder()
function stream(...chunks: string[]) {
  get.mockResolvedValue({ data: new ReadableStream<Uint8Array>({
    start(controller) { chunks.forEach(chunk => controller.enqueue(encoder.encode(chunk))); controller.close() },
  }) })
}
beforeEach(() => { vi.clearAllMocks() })

describe('bounded usage export', () => {
  it('assembles split UTF-8 frames and requires a matching completion count', async () => {
    const bytes = encoder.encode('{"type":"rows","items":[{"name":"中😀文"}]}\n{"type":"complete","rows":1}\n')
    get.mockResolvedValue({ data: new ReadableStream<Uint8Array>({
      start(c) { for (const value of bytes) c.enqueue(Uint8Array.of(value)); c.close() },
    }) })
    const rows = vi.fn()
    expect(await consumeUsageExport('/usage/export', {}, rows)).toBe(1)
    expect(rows).toHaveBeenCalledWith([{ name: '中😀文' }])
    expect(get).toHaveBeenCalledWith('/usage/export', expect.objectContaining({ responseType: 'stream', timeout: 0, adapter: expect.any(Function) }))
  })
  it.each([
    '{"type":"rows","items":[1]}\n',
    '{"type":"rows","items":[1]}\n{"type":"complete","rows":2}\n',
    '{"type":"error","message":"row limit"}\n',
    '{"type":"complete","rows":0}\n{"type":"rows","items":[1]}\n',
    '{invalid',
  ])('rejects interrupted or invalid streams', async (body) => {
    stream(body)
    await expect(consumeUsageExport('/admin/usage/export', {}, vi.fn())).rejects.toThrow()
  })
  it('handles an empty complete export', async () => {
    stream('{"type":"complete","rows":0}\n')
    expect(await consumeUsageExport('/usage/export', {}, vi.fn())).toBe(0)
  })
  it('aborts and releases the stream reader', async () => {
    const cancel = vi.fn()
    const body = new ReadableStream<Uint8Array>({ cancel })
    get.mockResolvedValue({ data: body })
    const controller = new AbortController(); controller.abort()
    await expect(consumeUsageExport('/usage/export', {}, vi.fn(), controller.signal)).rejects.toThrow()
    expect(cancel).toHaveBeenCalledOnce()
    expect(body.locked).toBe(false)
  })
  it('rejects a batch beyond the client memory boundary', async () => {
    stream('x'.repeat(8 * 1024 * 1024 + 1))
    await expect(consumeUsageExport('/usage/export', {}, vi.fn())).rejects.toThrow('batch is too large')
  })
})

describe('CSV formula protection', () => {
  it.each(['=HYPERLINK("https://example.test")', '+SUM(1)', '-1+1', '@SUM(1)', '  =1+1', '\tformula', '\rformula'])(
    'neutralizes %s', (value) => { expect(escapeUsageCSV(value)).toMatch(/^"'/) },
  )
  it('preserves numbers, dash, multilingual text and CSV quoting', () => {
    expect(escapeUsageCSV(12.34)).toBe('12.34')
    expect(escapeUsageCSV('-')).toBe('-')
    expect(escapeUsageCSV('中文')).toBe('中文')
    expect(escapeUsageCSV('a,"b"')).toBe('"a,""b"""')
    expect(escapeUsageCSV(null)).toBe('')
  })
})
