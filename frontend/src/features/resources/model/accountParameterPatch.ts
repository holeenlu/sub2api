import type { Account, UpdateAccountRequest } from '@/features/resources/api/accountTypes';

// A batch applies only edited configuration; it never clones the reference
// account, its credentials, creator or runtime observations into other rows.
export function accountParameterPatch(account: Account, payload: Record<string, unknown>, baseline?: Record<string, unknown>): Record<string, unknown> {
  const patch: Record<string, unknown> = {}
  const equal = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
  const jsonPatch = (old: Record<string, unknown>, next: Record<string, unknown>) => {
    const result: Record<string, unknown> = {}
    for (const key of new Set([...Object.keys(old), ...Object.keys(next)])) {
      if (/^(vendor_|passive_usage_|client_telemetry|upstream_billing_probe$|quota_.*used$)/.test(key)) continue
      if (/^(access_token|refresh_token|id_token|client_secret|password|cookie|api_key|private_key|account_uuid|org_uuid|chatgpt_account_id|session_key)$/i.test(key)) continue
      const value = next[key]
      if (equal(old[key], value)) continue
      if (!baseline && old[key] == null && (value == null || value === '' || (typeof value === 'object' && Object.keys(value).length === 0))) continue
      result[key] = value === undefined ? null : value
    }
    return result
  }
  const fields = {
    name: account.name,
    notes: account.notes,
    proxy_id: account.proxy_id,
    concurrency: account.concurrency,
    priority: account.priority,
    rate_multiplier: account.rate_multiplier,
    load_factor: account.load_factor,
    status: account.status,
    schedulable: account.schedulable,
    group_ids: account.group_ids,
    expires_at: account.expires_at,
    auto_pause_on_expired: account.auto_pause_on_expired,
    // Request-only switches use the normalized editor baseline, not extra.
    upstream_billing_probe_enabled: undefined,
    upstream_billing_rate_sync_enabled: undefined,
  } satisfies Partial<Record<keyof UpdateAccountRequest, unknown>>
  for (const [key, value] of Object.entries(fields)) {
    if (!(key in payload)) continue
    let current = baseline ? baseline[key] : value
    if (key === 'expires_at' && typeof current === 'string') current = Math.floor(new Date(current).getTime() / 1000)
    if (key === 'notes') current ??= ''
    if (['proxy_id', 'load_factor', 'expires_at'].includes(key)) current ??= 0
    if (!equal(current, payload[key])) patch[key] = payload[key]
  }
  for (const key of ['credentials', 'extra'] as const) {
    if (!payload[key]) continue
    const changes = jsonPatch((baseline?.[key] as Record<string, unknown> | undefined) ?? account[key] ?? {}, payload[key] as Record<string, unknown>)
    if (Object.keys(changes).length) patch[key] = changes
  }
  return patch
}
