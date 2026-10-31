import type { Account } from '@/features/resources/api/accountTypes';
import type { useI18n } from 'vue-i18n'

export const translate = ((key: string) => key) as ReturnType<typeof useI18n>['t']

/** Synthetic, fully typed account used only by resource boundary regressions. */
export function accountFixture(overrides: Partial<Account> = {}): Account {
  return {
    id: 7, name: 'fixture account', platform: 'antigravity', type: 'oauth',
    credentials: {}, proxy_id: null, concurrency: 1, priority: 50, status: 'active',
    error_message: null, last_used_at: null, expires_at: null, auto_pause_on_expired: false,
    created_at: '2026-10-25T00:00:00Z', updated_at: '2026-10-25T00:00:00Z',
    schedulable: true, rate_limited_at: null, rate_limit_reset_at: null, overload_until: null,
    temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null,
    ...overrides,
  }
}

/** Command tests supply only consumed ports; any unexpected builder access fails loudly. */
export function commandFixture<T extends object>(inputs: Partial<T>): T {
  return new Proxy(inputs, {
    get(target, property, receiver) {
      if (!(property in target)) throw new Error('Unprovided command-test input: ' + String(property))
      return Reflect.get(target, property, receiver)
    },
  }) as T
}
