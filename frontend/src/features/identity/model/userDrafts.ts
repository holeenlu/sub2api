import type { AdminUser, UpdateUserRequest } from '../api/profileTypes'
import type { UserAttributeValuesMap } from '../api/attributeTypes'
import type { usersAPI } from '../api/users'

export function newUserDraft() {
  return { email: '', password: '', username: '', notes: '', role: 'user' as 'user' | 'admin' | 'super_admin',
    balance: '' as string | number, concurrency: 1, rpm_limit: 0 }
}

export function userEditDraft(user?: AdminUser | null) {
  return { reset_totp: false, email: user?.email || '', password: '', username: user?.username || '',
    notes: user?.notes || '', role: user?.role || 'user' as AdminUser['role'], concurrency: user?.concurrency ?? 1,
    rpm_limit: user?.rpm_limit ?? 0, customAttributes: {} as UserAttributeValuesMap }
}

/** Creation remains one server command, including the existing authorized initial balance. */
export function createUserPayload(draft: ReturnType<typeof newUserDraft>): Parameters<typeof usersAPI.create>[0] {
  const { balance: rawBalance, ...rest } = draft
  const balance = String(rawBalance).trim()
  const payload: Parameters<typeof usersAPI.create>[0] = { ...rest }
  if (balance !== '' && draft.role !== 'super_admin') payload.balance = Number(balance)
  return payload
}

export function updateUserPayload(draft: ReturnType<typeof userEditDraft>): UpdateUserRequest {
  const payload: UpdateUserRequest = { email: draft.email, username: draft.username, notes: draft.notes,
    role: draft.role, concurrency: draft.concurrency, rpm_limit: draft.rpm_limit }
  if (draft.password.trim()) payload.password = draft.password.trim()
  if (draft.reset_totp) payload.reset_totp = true
  return payload
}
