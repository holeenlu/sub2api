import type { Account } from '@/features/resources/api/accountTypes';

export function accountFixture(overrides: Partial<Account> = {}): Account {
  return {
    id: 42, name: 'Resource fixture', platform: 'openai', type: 'oauth',
    credentials: {}, credentials_status: {}, extra: {}, proxy_id: 7,
    concurrency: 1, priority: 1, status: 'active', error_message: null,
    last_used_at: null, expires_at: null, auto_pause_on_expired: false,
    created_at: '2026-10-25T00:00:00Z', updated_at: '2026-10-25T00:00:00Z',
    schedulable: true, rate_limited_at: null, rate_limit_reset_at: null,
    overload_until: null, temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null,
    ...overrides
  }
}

