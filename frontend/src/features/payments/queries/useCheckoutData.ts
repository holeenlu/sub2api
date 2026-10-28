import { ref, watch } from 'vue'
import { paymentAPI } from '../api/payment'
import type { CheckoutInfoResponse } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
export function useCheckoutData() {
const services = usePaymentsServices()
const request = usePaymentRequest(() => services.scopeKey.value)
const loading = ref(true)
function emptyCheckout(): CheckoutInfoResponse { return {
  methods: {}, global_min: 0, global_max: 0,
  plans: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '',
  recharge_bonus_tiers: [], recharge_bonus_mode: 'bonus', recharge_bonus_notice: '',
} }
const checkout = ref(emptyCheckout())

async function load() {
  const ticket = request.begin()
  if (!ticket.isCurrent()) { loading.value = false; return null }
  loading.value = true
  try {
    const response = await paymentAPI.getCheckoutInfo(ticket)
    if (!ticket.isCurrent()) return null
    checkout.value = response.data
    return { data: response.data, ticket }
  } finally { if (ticket.isCurrent()) loading.value = false }
}
watch(services.scopeKey, () => { checkout.value = emptyCheckout(); loading.value = false }, { flush: 'sync' })
return { checkout, loading, load }
}
