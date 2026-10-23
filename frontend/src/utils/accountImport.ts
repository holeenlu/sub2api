// Both account import entry points reject retired grants instead of silently
// discarding their credentials or treating them as native authorization.
export function hasRetiredAccountAuthorization(row: unknown): boolean {
  if (!row || typeof row !== 'object' || Array.isArray(row)) return false
  const account = row as Record<string, unknown>
  const extra = account.extra
  return Object.prototype.hasOwnProperty.call(account, 'siwc_authorization') ||
    (extra !== null && typeof extra === 'object' && Object.prototype.hasOwnProperty.call(extra, 'openai_siwc'))
}
