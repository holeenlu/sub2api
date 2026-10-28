import { parseRfc3339Timestamp } from '@/shared/lib/parseTimestamp'

/** Internal BPS storage scopes are not public model names or Native limits. */
export function isBPSCooldownScope(scope: string): boolean {
  return scope.startsWith('openai:excel_bps:')
}

export interface AccountRateLimitScope {
  scope: string
  resetAt: string
}

/** Read only the established expiry field; this does not decide routing or eligibility. */
export function readActiveAccountRateLimits(value: unknown, now = Date.now()): AccountRateLimitScope[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return []
  const active: AccountRateLimitScope[] = []
  for (const [scope, record] of Object.entries(value)) {
    if (record === null || typeof record !== 'object' || Array.isArray(record)) continue
    const resetAt: unknown = Reflect.get(record, 'rate_limit_reset_at')
    if (typeof resetAt !== 'string') continue
    const deadline = parseRfc3339Timestamp(resetAt)
    if (deadline && deadline.getTime() > now) active.push({ scope, resetAt })
  }
  return active
}

export type BPSCooldownScope =
  | { kind: 'account' }
  | { kind: 'model'; model: string }
  | { kind: 'endpoint'; endpoint: string }
  | { kind: 'model_endpoint'; model: string; endpoint: string }
  | { kind: 'model_permission'; model: string }
  | { kind: 'unknown' }

function displayTarget(encoded: string): string | null {
  if (!/^[A-Za-z0-9_-]{1,1024}$/.test(encoded) || encoded.length % 4 === 1) return null
  try {
    const bytes = Uint8Array.from(atob(encoded.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(encoded.length / 4) * 4, '=')), c => c.charCodeAt(0))
    const value = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
    const roundtrip = new TextEncoder().encode(value)
    if (roundtrip.length !== bytes.length || roundtrip.some((byte, i) => byte !== bytes[i])) return null
    if (!value || value.length > 512 || value.trim() !== value) return null
    for (const character of value) {
      const code = character.charCodeAt(0)
      if (code < 0x20 || code === 0x7f || (code >= 0x202a && code <= 0x202e) ||
        (code >= 0x2066 && code <= 0x2069) || code === 0xfeff) return null
    }
    return value
  } catch { return null }
}

/** Present only the grammar emitted by the BPS store. Unknown variants never become model names. */
export function readBPSCooldownScope(key: string): BPSCooldownScope | null {
  if (!isBPSCooldownScope(key)) return null
  const value = key.slice('openai:excel_bps:'.length)
  if (value === '429') return { kind: 'account' }
  const permission = /^model_permission:([A-Za-z0-9_-]+)$/.exec(value)
  if (permission) {
    const model = displayTarget(permission[1])
    return model ? { kind: 'model_permission', model } : { kind: 'unknown' }
  }
  const limited = /^429(?::model:([A-Za-z0-9_-]+))?(?::endpoint:([A-Za-z0-9_-]+))?$/.exec(value)
  if (!limited) return { kind: 'unknown' }
  const model = limited[1] ? displayTarget(limited[1]) : null
  const endpoint = limited[2] ? displayTarget(limited[2]) : null
  if ((limited[1] && !model) || (limited[2] && (!endpoint || !endpoint.startsWith('/') || /[?#]/.test(endpoint)))) return { kind: 'unknown' }
  if (model && endpoint) return { kind: 'model_endpoint', model, endpoint }
  if (model) return { kind: 'model', model }
  if (endpoint) return { kind: 'endpoint', endpoint }
  return { kind: 'unknown' }
}
