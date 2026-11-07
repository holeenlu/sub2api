import { apiClient } from '@/shared/http'

export interface BalanceHistoryItem {
  id: string
  type: 'balance' | 'admin_balance' | 'affiliate_balance' | 'concurrency' | 'admin_concurrency' | 'subscription'
  value: number
  source: 'admin' | 'payment' | 'redeem' | 'affiliate'
  notes: string
  occurred_at: string
}

export interface BalanceHistoryPage {
  items: BalanceHistoryItem[]
  total: number
  total_recharged: number
}

export interface BalanceHistoryFilter {
  category: 'balance' | 'other'
  direction: '' | 'increase' | 'decrease'
  source: '' | BalanceHistoryItem['source']
}

export async function getBalanceHistory(params: BalanceHistoryFilter & { page: number; page_size: number }, options?: { signal?: AbortSignal }): Promise<BalanceHistoryPage> {
  const { data } = await apiClient.get<BalanceHistoryPage>('/user/balance-history', { params, signal: options?.signal })
  return data
}
