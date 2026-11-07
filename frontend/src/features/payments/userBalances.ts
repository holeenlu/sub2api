import { apiClient } from '@/shared/http'
import type { PaginatedResponse, FetchOptions } from '@/shared/http/requestTypes'
import type { AdminUser } from '@/features/identity'

// Scoped administrators can receive an id-only acknowledgement from the server.
export type AdminBalanceUpdateResult = AdminUser | Pick<AdminUser, 'id'>

// The administrative redeem-code ledger differs from the personal balance ledger.
export interface AdminBalanceHistoryItem {
  id: number
  code: string
  type: string
  value: number
  status: string
  used_by: number | null
  used_at: string | null
  created_at: string
  group_id: number | null
  validity_days: number
  notes: string
  user?: Pick<AdminUser, 'id' | 'email'> | null
  group?: { id: number; name: string } | null
}

export interface AdminBalanceHistoryResponse extends PaginatedResponse<AdminBalanceHistoryItem> {
  total_recharged: number
}

export async function updateBalance(
  id: number,
  balance: number,
  operation: 'set' | 'add' | 'subtract' = 'set',
  notes?: string
): Promise<AdminBalanceUpdateResult> {
  const { data } = await apiClient.post<AdminBalanceUpdateResult>(`/admin/users/${id}/balance`, {
    balance,
    operation,
    notes: notes || ''
  })
  return data
}

export async function getUserBalanceHistory(
  id: number,
  page: number = 1,
  pageSize: number = 20,
  type?: string,
  options?: FetchOptions
): Promise<AdminBalanceHistoryResponse> {
  const params: { page: number; page_size: number; type?: string } = {
    page,
    page_size: pageSize
  }
  if (type) params.type = type
  const { data } = await apiClient.get<AdminBalanceHistoryResponse>(`/admin/users/${id}/balance-history`, {
    params,
    ...options
  })
  return data
}
