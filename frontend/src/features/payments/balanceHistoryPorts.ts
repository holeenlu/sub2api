import type { InjectionKey } from 'vue'
import type { User } from '@/features/identity/api/profileTypes';
export type BalanceHistoryAccount = Pick<User, 'id' | 'email' | 'username' | 'balance'>
export type BalanceHistoryAccountReader = (options: { signal: AbortSignal }) => Promise<BalanceHistoryAccount>
export const balanceHistoryAccountReaderKey: InjectionKey<BalanceHistoryAccountReader> = Symbol('BalanceHistoryAccountReader')
