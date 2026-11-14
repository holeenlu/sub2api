import { defineStore } from 'pinia'
import { onScopeDispose, ref } from 'vue'
import { vendorAPI } from './api'
import type { Vendor } from './types'

/** App binds a logical session key before router/component refresh; tokens are never keys. */
export const useSupplyStore = defineStore('supply', () => {
  const vendor = ref<Vendor | null>(null)
  const scopeKey = ref<string | null>(null)
  const loading = ref(false)
  let generation = 0
  let controller: AbortController | undefined
  function clear() {
    generation++
    controller?.abort()
    controller = undefined
    vendor.value = null
    loading.value = false
  }
  function setSessionScope(value: string | null) {
    if (value === scopeKey.value) return
    clear()
    scopeKey.value = value
  }
  async function refresh(options: { signal?: AbortSignal } = {}) {
    const scope = scopeKey.value
    if (scope === null) throw new DOMException('No active supplier session', 'AbortError')
    controller?.abort()
    const request = new AbortController()
    controller = request
    const epoch = ++generation
    const abort = () => request.abort()
    options.signal?.addEventListener('abort', abort, { once: true })
    if (options.signal?.aborted) abort()
    const current = () => epoch === generation && scope === scopeKey.value && !request.signal.aborted
    loading.value = true
    try {
      if (!current()) throw new DOMException('Supplier refresh cancelled', 'AbortError')
      const value = await vendorAPI.me(request.signal)
      if (!current()) throw new DOMException('Supplier refresh cancelled', 'AbortError')
      vendor.value = value
      return value
    } catch (error) {
      if (current()) vendor.value = null
      if (!current()) throw new DOMException('Supplier refresh cancelled', 'AbortError')
      throw error
    } finally {
      options.signal?.removeEventListener('abort', abort)
      if (epoch === generation) loading.value = false
    }
  }
  onScopeDispose(clear)
  return { vendor, scopeKey, loading, refresh, clear, setSessionScope }
})
