import type { ApiKey } from '../api/types'
export function keyFixture(id = 1): ApiKey {
  return { id, user_id: 1, name: 'Client', key: 'sk-private-key-12345', group_id: 7, status: 'active',
    ip_whitelist: ['10.0.0.0/8'], ip_blacklist: [], quota: 10, quota_used: 3, expires_at: null,
    created_at: '', updated_at: '', last_used_at: null, last_used_ip: null, current_concurrency: 0,
    rate_limit_5h: 2, rate_limit_1d: 0, rate_limit_7d: 0, usage_5h: 1, usage_1d: 0, usage_7d: 0,
    window_5h_start: null, window_1d_start: null, window_7d_start: null, reset_5h_at: null, reset_1d_at: null, reset_7d_at: null }
}
