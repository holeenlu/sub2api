/** Browser image references only; data payload decoding belongs to the serving endpoint. */
export function sanitizeImageReference(value) {
  const trimmed = value.trim()
  if (!trimmed || /[\r\n\t\\]/.test(trimmed)) return ''
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) return trimmed
  if (/^data:image\//i.test(trimmed)) return trimmed

  // Require an explicit authority before URL parsing: the browser repairs forms
  // such as https:///host, whereas the server treats their host as missing.
  const authority = /^https?:\/\/([^/?#]+)/i.exec(trimmed)?.[1]
  if (!authority || authority.includes('@')) return ''
  try {
    const parsed = new URL(trimmed)
    if (!parsed.hostname || parsed.username || parsed.password) return ''
    return trimmed
  } catch {
    return ''
  }
}
