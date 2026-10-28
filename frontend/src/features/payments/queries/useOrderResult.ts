import { computed, ref, watch, onScopeDispose } from 'vue'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
import { isPendingStatus, type ResolvedOrder } from '../model/orderResult'
import { PAYMENT_RECOVERY_STORAGE_KEY, readPaymentRecoverySnapshot } from '../model/paymentFlow'
export interface OrderResultLocation { orderId: number; resumeToken: string; outTradeNo: string; legacyReturn: boolean }
/** Public credentials and private session reads have independent cancellation/response ownership. */
export function useOrderResult(location: () => OrderResultLocation) {
  const services = usePaymentsServices()
  const routeKey = computed(() => JSON.stringify(location()))
  const publicRequest = usePaymentRequest(() => 'public:' + routeKey.value)
  const privateRequest = usePaymentRequest(() => services.scopeKey.value === null ? null : services.scopeKey.value + ':' + routeKey.value)
  const context = computed(() => location().resumeToken || !location().orderId ? routeKey.value : routeKey.value + ':' + services.scopeKey.value)
  const order = ref<ResolvedOrder | null>(null), loading = ref(true), privateOwnerScope = ref<string | null>(null)
  let version = 0, disposed = false, attempts = 0, balanceRefreshed = false
  let timer: ReturnType<typeof setTimeout> | undefined
  function stop(): void { clearTimeout(timer); timer = undefined; publicRequest.cancel(); privateRequest.cancel(); ++version }
  function clearOwnedRecovery(value: ResolvedOrder): void {
    const raw = localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
    const stored = readPaymentRecoverySnapshot(raw)
    if (stored && value.out_trade_no && stored.outTradeNo === value.out_trade_no && !isPendingStatus(value.status)
      && localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY) === raw) localStorage.removeItem(PAYMENT_RECOVERY_STORAGE_KEY)
  }
  async function read(): Promise<{ order: ResolvedOrder; privateScope: string | null } | null> {
    const target = { ...location() }
    if (target.resumeToken) {
      const ticket = publicRequest.begin()
      try {
        const value = await services.resolvePublicOrder({ resumeToken: target.resumeToken }, ticket)
        if (ticket.isCurrent()) return { order: value, privateScope: null }
      } catch { if (!ticket.isCurrent()) return null }
      // A signed-link failure may still have a separately authorized private order ID.
    }
    if (target.orderId > 0 && services.scopeKey.value !== null) {
      const ticket = privateRequest.begin(), userId = services.user.value?.id, privateScope = services.scopeKey.value
      try {
        const response = await paymentAPI.getOrder(target.orderId, ticket)
        if (!ticket.isCurrent()) return null
        if (response.data.user_id === userId && response.data.id === target.orderId) return { order: response.data, privateScope }
      } catch { if (!ticket.isCurrent()) return null }
    }
    if (target.outTradeNo && (target.legacyReturn || target.orderId > 0)) {
      if (services.scopeKey.value !== null) {
        const ticket = privateRequest.begin(), userId = services.user.value?.id, privateScope = services.scopeKey.value
        try {
          const verified = await paymentAPI.verifyOrder(target.outTradeNo, ticket)
          if (!ticket.isCurrent()) return null
          if (verified.data.user_id === userId && verified.data.out_trade_no === target.outTradeNo) return { order: verified.data, privateScope }
        } catch { if (!ticket.isCurrent()) return null }
      }
      const ticket = publicRequest.begin()
      try {
        const value = await services.resolvePublicOrder({ outTradeNo: target.outTradeNo }, ticket)
        if (ticket.isCurrent()) return { order: value, privateScope: null }
      } catch { /* Unknown remains unconfirmed, never inferred from URL fields. */ }
    }
    return null
  }
  async function refresh(captured: number): Promise<void> {
    const resolved = await read()
    const value = resolved?.order ?? null
    if (disposed || version !== captured) return
    if (value) {
      if (resolved?.privateScope !== null && resolved?.privateScope !== services.scopeKey.value) return
      order.value = value; privateOwnerScope.value = resolved?.privateScope ?? null; clearOwnedRecovery(value)
      if (resolved?.privateScope !== null && resolved?.privateScope === services.scopeKey.value && !balanceRefreshed && 'user_id' in value && value.user_id === services.user.value?.id
        && value.order_type === 'balance' && value.status === 'COMPLETED') {
        balanceRefreshed = true; void services.refreshUser().catch(() => {})
      }
    }
    loading.value = false
    const target = location()
    const hasLookup = !!target.resumeToken || target.orderId > 0 || (!!target.outTradeNo && target.legacyReturn)
    if (hasLookup && (!value || isPendingStatus(value.status)) && attempts < 15) {
      timer = setTimeout(() => { attempts++; void refresh(captured) }, 2000)
    }
  }
  watch(context, () => {
    stop(); order.value = null; privateOwnerScope.value = null; loading.value = true; attempts = 0; balanceRefreshed = false
    void refresh(version)
  }, { immediate: true, flush: 'sync' })
  // A public response survives a login change; any private projection never does.
  watch(services.scopeKey, () => {
    if (privateOwnerScope.value !== null) {
      stop(); order.value = null; privateOwnerScope.value = null; loading.value = true; attempts = 0; balanceRefreshed = false
      void refresh(version)
    }
  }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; stop() })
  return { order, loading }
}
