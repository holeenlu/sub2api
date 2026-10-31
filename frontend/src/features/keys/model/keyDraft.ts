import { computed, type Ref } from 'vue'
import type { ApiKey, CreateApiKeyRequest, UpdateApiKeyRequest } from '../api/types'

const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function emptyKeyDraft() { return {
  name: '',
  group_id: null as number | null,
  status: 'active' as 'active' | 'inactive',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  // Quota settings (empty = unlimited)
  quota: null as number | null,
  // Rate limit settings
  enable_rate_limit: false,
  rate_limit_5h: null as number | null,
  rate_limit_1d: null as number | null,
  rate_limit_7d: null as number | null,
  enable_expiration: false,
  expiration_preset: '30' as '7' | '30' | '90' | 'custom',
  expiration_date: ''
} }
export type KeyDraft = ReturnType<typeof emptyKeyDraft>

export function editKeyDraft(key: ApiKey): KeyDraft {
  const hasIPRestriction = (key.ip_whitelist?.length > 0) || (key.ip_blacklist?.length > 0)
  const hasExpiration = !!key.expires_at
  return {
    name: key.name,
    group_id: key.group_id,
    status: key.status === 'quota_exhausted' || key.status === 'expired' ? 'inactive' : key.status,
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: hasIPRestriction,
    ip_whitelist: (key.ip_whitelist || []).join('\n'),
    ip_blacklist: (key.ip_blacklist || []).join('\n'),
    quota: key.quota > 0 ? key.quota : null,
    enable_rate_limit: (key.rate_limit_5h > 0) || (key.rate_limit_1d > 0) || (key.rate_limit_7d > 0),
    rate_limit_5h: key.rate_limit_5h || null,
    rate_limit_1d: key.rate_limit_1d || null,
    rate_limit_7d: key.rate_limit_7d || null,
    enable_expiration: hasExpiration,
    expiration_preset: 'custom',
    expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
  }
}

export function keyDraftField<K extends keyof KeyDraft>(draft: Ref<KeyDraft>, field: K) {
  return computed({ get: () => draft.value[field], set: (value: KeyDraft[K]) => { draft.value = { ...draft.value, [field]: value } } })
}
export function customKeyErrorKey(draft: KeyDraft): string {
  if (!draft.use_custom_key || !draft.custom_key) return ''
  if (draft.custom_key.length < 16) return 'keys.customKeyTooShort'
  return /^[a-zA-Z0-9_-]+$/.test(draft.custom_key) ? '' : 'keys.customKeyInvalidChars'
}
export function validateKeyDraft(draft: KeyDraft, editing: boolean): string {
  if (draft.group_id === null) return 'keys.groupRequired'
  if (!editing && draft.use_custom_key) {
    if (!draft.custom_key) return 'keys.customKeyRequired'
    if (customKeyErrorKey(draft)) return customKeyErrorKey(draft)
  }
  for (const field of ['quota', 'rate_limit_5h', 'rate_limit_1d', 'rate_limit_7d'] as const) {
    const value = draft[field]
    if (value !== null && String(value) !== '' && (!Number.isFinite(Number(value)) || Number(value) < 0)) return 'keys.bulkEdit.invalidLimit'
  }
  if (draft.enable_expiration && draft.expiration_date && !Number.isFinite(Date.parse(draft.expiration_date))) return 'keys.bulkEdit.invalidExpiration'
  return ''
}
const positive = (value: number | null): number => Number(value) > 0 ? Number(value) : 0
const parseIPs = (value: string): string[] => value.split('\n').map(ip => ip.trim()).filter(Boolean)
function restrictions(draft: KeyDraft) {
  return {
    ip_whitelist: draft.enable_ip_restriction ? parseIPs(draft.ip_whitelist) : [],
    ip_blacklist: draft.enable_ip_restriction ? parseIPs(draft.ip_blacklist) : [],
    quota: positive(draft.quota),
    rate_limit_5h: draft.enable_rate_limit ? positive(draft.rate_limit_5h) : 0,
    rate_limit_1d: draft.enable_rate_limit ? positive(draft.rate_limit_1d) : 0,
    rate_limit_7d: draft.enable_rate_limit ? positive(draft.rate_limit_7d) : 0
  }
}
export function createKeyPayload(draft: KeyDraft, now = Date.now()): CreateApiKeyRequest {
  const payload: CreateApiKeyRequest = { name: draft.name, group_id: draft.group_id, ...restrictions(draft) }
  if (draft.use_custom_key) payload.custom_key = draft.custom_key
  if (draft.enable_expiration && draft.expiration_date) payload.expires_in_days = Math.max(1, Math.ceil((Date.parse(draft.expiration_date) - now) / 86400000))
  return payload
}
export function updateKeyPayload(draft: KeyDraft, key: ApiKey): UpdateApiKeyRequest {
  const payload: UpdateApiKeyRequest = {
    name: draft.name, group_id: draft.group_id, ...restrictions(draft),
    expires_at: draft.enable_expiration && draft.expiration_date ? new Date(draft.expiration_date).toISOString() : ''
  }
  if ((key.status !== 'quota_exhausted' && key.status !== 'expired') || draft.status === 'active') payload.status = draft.status
  return payload
}
export function expirationAfterDays(days: number): string {
  const date = new Date(); date.setDate(date.getDate() + days)
  return formatDateTimeLocal(date.toISOString())
}
