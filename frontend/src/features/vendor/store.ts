import { defineStore } from 'pinia'
import { ref } from 'vue'
import { vendorAPI } from './api'
import type { Vendor } from './types'
export const useVendorStore = defineStore('vendor', () => {
  const vendor = ref<Vendor | null>(null)
  let generation = 0
  async function refresh() {
    const epoch = ++generation
    try {
      const value = await vendorAPI.me()
      if (epoch === generation) vendor.value = value
      return value
    } catch (e) {
      if (epoch === generation) vendor.value = null
      throw e
    }
  }
  function clear() {
    generation++
    vendor.value = null
  }
  return { vendor, refresh, clear }
})
