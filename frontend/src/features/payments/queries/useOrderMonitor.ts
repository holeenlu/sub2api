import { computed, onScopeDispose, watch } from 'vue'
import { paymentAPI } from '../api/payment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
export function useOrderMonitor(options: {
  orderId: () => number; recoverPending: () => boolean; isTerminal: () => boolean;
  onOrder: (order: PaymentOrder) => void; onInvalidate?: () => void
}) {
  const services = usePaymentsServices()
  const context = computed(() => services.scopeKey.value && options.orderId() > 0 ? services.scopeKey.value + '|order:' + options.orderId() : null)
  const request = usePaymentRequest(() => context.value)
  let timer: ReturnType<typeof setInterval> | undefined
  let inFlight = false
  let verifyAttempts = 0, lastVerifyAt = 0
  function stop(): void { clearInterval(timer); timer = undefined; request.cancel(); inFlight = false }
  async function poll(): Promise<void> {
    if (inFlight || options.isTerminal() || !context.value) return
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    const orderId = options.orderId()
    inFlight = true
    try {
      const response = await paymentAPI.getOrder(orderId, ticket)
      if (!ticket.isCurrent() || options.isTerminal()) return
      let order = response.data
      if (!order) return
      const outTradeNo = String(order.out_trade_no || '').trim()
      if (options.recoverPending() && outTradeNo && order.status === 'PENDING'
        && verifyAttempts < 6 && Date.now() - lastVerifyAt >= 15000) {
        lastVerifyAt = Date.now(); verifyAttempts += 1
        try {
          const verified = await paymentAPI.verifyOrder(outTradeNo, ticket)
          if (!ticket.isCurrent() || options.isTerminal()) return
          order = verified.data ?? order
        } catch { if (!ticket.isCurrent()) return }
      }
      if (ticket.isCurrent() && !options.isTerminal()) options.onOrder(order)
    } catch { /* A read failure is not payment failure and never recreates an order. */ }
    finally { if (ticket.isCurrent()) inFlight = false }
  }
  function start(): void {
    stop(); verifyAttempts = 0; lastVerifyAt = 0
    if (context.value && !options.isTerminal()) timer = setInterval(() => { void poll() }, 3000)
  }
  watch(context, () => { stop(); verifyAttempts = 0; lastVerifyAt = 0; options.onInvalidate?.() }, { flush: 'sync' })
  onScopeDispose(stop)
  return { start, stop, poll }
}
