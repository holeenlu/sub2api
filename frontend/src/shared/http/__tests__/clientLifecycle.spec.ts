import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient, configureHttpClient } from '../client'

const adapter = apiClient.defaults.adapter
beforeEach(() => {
  localStorage.clear()
  configureHttpClient({})
  apiClient.defaults.adapter = async config => ({ config, data: { code: 0, data: {} }, status: 200, statusText: 'OK', headers: {} })
})
afterEach(() => { configureHttpClient({}); apiClient.defaults.adapter = adapter })

describe('application HTTP lifecycle ownership', () => {
  it('detaches an unmounted host without detaching a replacement host', async () => {
    const first = vi.fn(async () => undefined)
    const second = vi.fn(async () => undefined)
    const disposeFirst = configureHttpClient({ acceptResponse: first })
    await apiClient.get('/probe')
    expect(first).toHaveBeenCalledOnce()
    const disposeSecond = configureHttpClient({ acceptResponse: second })
    disposeFirst()
    disposeFirst()
    await apiClient.get('/probe')
    expect(first).toHaveBeenCalledOnce()
    expect(second).toHaveBeenCalledOnce()
    disposeSecond()
    await apiClient.get('/probe')
    expect(second).toHaveBeenCalledOnce()
  })
})
