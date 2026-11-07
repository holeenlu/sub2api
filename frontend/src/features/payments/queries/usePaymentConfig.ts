import { ref, watch } from 'vue'
import type { PaymentConfig } from '../api/types'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
export function usePaymentConfig() {
  const services = usePaymentsServices()
  const request = usePaymentRequest(() => services.scopeKey.value)
  const config = ref<PaymentConfig | null>(null), loading = ref(false)
  let current: Promise<PaymentConfig | null> | null = null
  async function load(force = false): Promise<PaymentConfig | null> {
    if (current) return current
    if (config.value && !force) return config.value
    const ticket = request.begin()
    if (!ticket.isCurrent()) return null
    loading.value = true
    const pending = (async () => {
      try {
        const response = await Promise.resolve().then(() => paymentAPI.getConfig(ticket))
        if (!ticket.isCurrent()) return null
        config.value = response.data; return config.value
      } catch { return null }
      finally { if (ticket.isCurrent()) { current = null; loading.value = false } }
    })()
    current = pending
    return pending
  }
  watch(services.scopeKey, () => { config.value = null; current = null; loading.value = false }, { flush: 'sync' })
  return { config, loading, load }
}
