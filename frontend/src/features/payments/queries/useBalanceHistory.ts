import { computed, inject, ref, watch } from 'vue'
import { getBalanceHistory, type BalanceHistoryItem } from '../api/balanceHistory'
import { balanceHistoryAccountReaderKey, type BalanceHistoryAccount } from '../balanceHistoryPorts'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
export function useBalanceHistory(show: () => boolean) {
  const services = usePaymentsServices(), readAccount = inject(balanceHistoryAccountReaderKey)
  if (!readAccount) throw new Error('BalanceHistoryAccountReader must be provided by app')
  const accountReader = readAccount
  const context = computed(() => show() && services.scopeKey.value !== null && services.user.value ? services.scopeKey.value + ':' + services.user.value.id : null)
  const request = usePaymentRequest(() => context.value)
  const items = ref<BalanceHistoryItem[]>([]), account = ref<BalanceHistoryAccount | null>(null), totalRecharged = ref<number | null>(null)
  const total = ref(0), page = ref(1), pageSize = 15, loading = ref(false), failed = ref(false)
  const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
  async function loadHistory(nextPage: number): Promise<void> {
    const ticket = request.begin(), userId = services.user.value?.id
    if (!ticket.isCurrent()) return
    loading.value = true; failed.value = false; items.value = []; page.value = nextPage
    try {
      const [result, profile] = await Promise.all([
        getBalanceHistory({ category: 'balance', direction: '', source: '', page: nextPage, page_size: pageSize }, ticket),
        account.value ? Promise.resolve(account.value) : accountReader(ticket),
      ])
      if (!ticket.isCurrent()) return
      if (profile.id !== userId) throw new Error('Account changed')
      account.value = profile; items.value = result.items; total.value = result.total; totalRecharged.value = Number.isFinite(result.total_recharged) ? result.total_recharged : null
    } catch { if (ticket.isCurrent()) failed.value = true }
    finally { if (ticket.isCurrent()) loading.value = false }
  }
  watch(context, () => {
    items.value = []; account.value = null; totalRecharged.value = null; total.value = 0; page.value = 1; failed.value = false; loading.value = false
    if (context.value !== null) void loadHistory(1)
  }, { immediate: true, flush: 'sync' })
  return { items, account, totalRecharged, total, page, pages, loading, failed, loadHistory }
}
