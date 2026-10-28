import axios, { type AxiosAdapter } from 'axios'
import { apiClient } from './client'

// Axios keeps the same credential, scope, step-up and refresh interceptors for SSE.
// Decode an unsuccessful stream before those interceptors inspect its error envelope.
const streamAdapter: AxiosAdapter = async config => {
  try {
    return await axios.getAdapter('fetch')(config)
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.data instanceof ReadableStream) {
      let text: string
      try { text = await new Response(error.response.data).text() }
      catch {
        if (config.signal?.aborted) throw new axios.CanceledError('Stream request cancelled', config)
        throw error
      }
      try { error.response.data = JSON.parse(text) as unknown }
      catch { error.response.data = { message: text || error.message } }
    }
    throw error
  }
}

/** Management SSE uses the one shared client; callers own cancellation after headers. */
export async function postEventStream(path: string, payload: unknown, signal: AbortSignal): Promise<Response> {
  const response = await apiClient.post<ReadableStream<Uint8Array>>(path, payload, {
    adapter: streamAdapter, responseType: 'stream', signal,
    headers: { Accept: 'text/event-stream' }
  })
  const headers = new Headers()
  for (const [key, value] of Object.entries(response.headers)) {
    if (typeof value === 'string') headers.set(key, value)
  }
  return new Response(response.data, { status: response.status, statusText: response.statusText, headers })
}
