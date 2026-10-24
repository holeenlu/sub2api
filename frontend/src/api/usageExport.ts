import axios from 'axios'
import { apiClient } from './client'

// Consume bounded NDJSON batches. The completion frame, not socket EOF, is the
// success signal, so a canceled/timed-out export can never download a partial file.
export async function consumeUsageExport<T>(
  endpoint: '/usage/export' | '/admin/usage/export',
  params: object,
  onRows: (rows: T[]) => void,
  signal?: AbortSignal,
): Promise<number> {
  const response = await apiClient.get<ReadableStream<Uint8Array>>(endpoint, {
    params, signal, timeout: 0, responseType: 'stream',
    // Keep the shared authentication, permission and step-up interceptors. HTTP
    // error bodies need decoding before those interceptors examine their reason.
    adapter: async (config) => {
      try { return await axios.getAdapter('fetch')(config) }
      catch (error) {
        if (axios.isAxiosError(error) && error.response?.data instanceof ReadableStream) {
          const reader = error.response.data.getReader()
          const decoder = new TextDecoder()
          let body = ''
          try {
            while (body.length <= 65536) {
              const { value, done } = await reader.read()
              if (done) break
              body += decoder.decode(value, { stream: true })
            }
            if (body.length <= 65536) error.response.data = JSON.parse(body + decoder.decode())
          } catch { /* The shared interceptor handles malformed HTTP errors. */ }
          finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
        }
        throw error
      }
    },
  })
  if (!(response.data instanceof ReadableStream)) throw new Error('Invalid export stream')
  const reader = response.data.getReader()
  const decoder = new TextDecoder()
  let pending = '', count = 0, bytes = 0, completed = false
  const consume = (line: string) => {
    if (!line.trim()) return
    if (completed) throw new Error('Unexpected data after export completion')
    const frame = JSON.parse(line)
    if (frame.type === 'error') throw new Error(String(frame.message || 'Export failed'))
    if (frame.type === 'rows' && Array.isArray(frame.items)) {
      count += frame.items.length
      onRows(frame.items)
    } else if (frame.type === 'complete' && frame.rows === count) completed = true
    else throw new Error('Invalid export frame')
  }
  try {
    while (true) {
      signal?.throwIfAborted()
      const { value, done } = await reader.read()
      if (done) break
      bytes += value.byteLength
      if (bytes > 256 * 1024 * 1024) throw new Error('Export exceeds browser size limit; narrow the date range')
      pending += decoder.decode(value, { stream: true })
      let end: number
      while ((end = pending.indexOf('\n')) !== -1) {
        if (end > 8 * 1024 * 1024) throw new Error('Export batch is too large')
        consume(pending.slice(0, end)); pending = pending.slice(end + 1)
      }
      if (pending.length > 8 * 1024 * 1024) throw new Error('Export batch is too large')
    }
    pending += decoder.decode()
    if (pending.trim()) consume(pending)
    if (!completed) throw new Error('Export ended before completion')
    return count
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
}

// Quoting alone does not stop spreadsheet formulas. Escape leading whitespace
// before formula prefixes as well, while retaining the existing dash placeholder.
export function escapeUsageCSV(value: unknown): string {
  if (value == null) return ''
  const str = String(value)
  const escaped = str.replace(/"/g, '""')
  if (str === '-') return str
  if (/^[\t\r]|^\s*[=+\-@]/.test(str)) return '"' + "'" + escaped + '"'
  if (/[,"\n\r]/.test(str)) return '"' + escaped + '"'
  return str
}
