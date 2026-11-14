import type { CreateProxyRequest, ProxyProtocol, UpdateProxyRequest } from '@/features/resources/api/proxyTypes';

export interface ProxyDraft {
  name: string; protocol: ProxyProtocol; host: string; port: number
  username: string; password: string; expires_at: string
  fallback_mode: 'none' | 'proxy' | 'direct'; backup_proxy_id: number | null; expiry_warn_days: number
}
function connectionFields(form: ProxyDraft) {
  return { name: form.name.trim(), protocol: form.protocol, host: form.host.trim(), port: form.port,
    expires_at: form.expires_at ? Math.floor(new Date(form.expires_at).getTime() / 1000) : null,
    fallback_mode: form.fallback_mode,
    backup_proxy_id: form.fallback_mode === 'proxy' ? form.backup_proxy_id : null,
    expiry_warn_days: form.expiry_warn_days }
}
export function proxyCreatePayload(form: ProxyDraft): CreateProxyRequest {
  return { ...connectionFields(form), username: form.username.trim() || null, password: form.password.trim() || null }
}
/** Omission preserves redacted credentials; an explicit dirty blank clears a password. */
export function proxyUpdatePayload(form: ProxyDraft & { status: 'active' | 'inactive' }, options: { passwordDirty: boolean; isSuperAdmin: boolean }): UpdateProxyRequest {
  const value: UpdateProxyRequest = { ...connectionFields(form), status: form.status }
  if (options.isSuperAdmin || form.username.trim()) value.username = form.username.trim()
  if (options.passwordDirty) value.password = form.password.trim()
  return value
}
