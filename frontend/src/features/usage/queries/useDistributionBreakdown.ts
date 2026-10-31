import { ref, watch } from 'vue'
import { getUserBreakdown, type UserBreakdownParams } from '../adminDashboard'
import type { UserBreakdownItem } from '../api/types'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
export function useDistributionBreakdown(filters: () => UserBreakdownParams, enabled: () => boolean) {
  const services = useUsageServices(), request = useUsageRequest(services.scopeKey)
  const expandedKey = ref<string | null>(null), breakdownItems = ref<UserBreakdownItem[]>([]), breakdownLoading = ref(false)
  function close(): void { request.cancel(); expandedKey.value = null; breakdownItems.value = []; breakdownLoading.value = false }
  async function toggle(key: string, selection: Partial<UserBreakdownParams>): Promise<void> {
    if (!enabled()) return
    if (expandedKey.value === key) { close(); return }
    const ticket = request.begin(), params = { ...filters(), ...selection }
    if (!ticket.isCurrent()) return
    expandedKey.value = key; breakdownItems.value = []; breakdownLoading.value = true
    try { const result = await getUserBreakdown(params, ticket); if (ticket.isCurrent()) breakdownItems.value = result.users ?? [] }
    catch { if (ticket.isCurrent()) breakdownItems.value = [] }
    finally { if (ticket.isCurrent()) breakdownLoading.value = false }
  }
  watch(() => JSON.stringify([filters(), enabled()]), close, { flush: 'sync' })
  watch(services.scopeKey, close, { flush: 'sync' })
  return { expandedKey, breakdownItems, breakdownLoading, toggle }
}
