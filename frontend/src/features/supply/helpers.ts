import { readApiError } from '@/shared/lib/apiError'
// Readonly usage-mode type owner: usage; supplier permissions remain separate.
import type { UsageAuthorizationMode } from '@/features/usage'
import type { Vendor, VendorAccount, OpenAIAuthorizationMode, VendorProxy, ProxyQuality, Metrics, ResourceScope } from './types'

export const openAIAuthorizationModes = ['native', 'bps'] as const
// Historical usage modes do not expand the supplier authorization policy.
export const usageAuthorizationModes: readonly UsageAuthorizationMode[] = ['native', 'bps', 'cloud']

// This is the effective policy from the server. Only legacy missing fields default
// to native; an explicit empty whitelist must remain empty.
export function vendorOpenAIModes(vendor?: Vendor, account?: VendorAccount): OpenAIAuthorizationMode[] {
  if (!vendor?.platforms?.includes('openai')) return []
  const modes: OpenAIAuthorizationMode[] = vendor.openai_authorization_modes ?? ['native']
  // Account details may reflect a parent revocation newer than the cached policy.
  return modes.filter(mode =>
    !account?.authorizations?.some(grant => grant.mode === mode && grant.status === 'policy_revoked'))
}

export function accountOpenAIModes(account: VendorAccount): OpenAIAuthorizationMode[] {
  if (account.platform !== 'openai') return []
  if (account.authorization_modes) return account.authorization_modes
  if (account.authorizations)
    return account.authorizations.filter(grant => grant.authorized).map(grant => grant.mode)
  return account.type === 'oauth' ? ['native'] : []
}

export function resourceOwnerScope(scope: ResourceScope, vendorId?: number): ResourceScope {
  if (!scope.children && scope.vendorId !== 0) return scope
  if (!vendorId || !Number.isSafeInteger(vendorId) || vendorId < 1)
    throw new Error('A resource owner is required')
  return { admin: scope.admin, readOnly: scope.readOnly, vendorId }
}

export function isVendorOrigin(
  settings: { vendor_portal_origin?: string } | null | undefined,
  origin = window.location.origin,
): boolean {
  const configured = settings?.vendor_portal_origin
  if (!configured) return false
  try {
    const url = new URL(configured)
    return (
      url.origin === origin &&
      url.pathname === '/' &&
      !url.search &&
      !url.hash &&
      !url.username &&
      !url.password
    )
  } catch {
    return false
  }
}
export function safePortalRedirect(value: unknown): string {
  const fallback = '/vendor/accounts'
  if (
    typeof value !== 'string' ||
    !value.startsWith('/vendor/') ||
    value.includes(String.fromCharCode(92))
  )
    return fallback
  try {
    const url = new URL(value, 'https://supplier.invalid')
    return url.origin === 'https://supplier.invalid' &&
      /^\/vendor\/(accounts|proxies|usage|settlements|profile|platform|children|child-accounts|child-proxies|child-usage|payables)$/.test(
        url.pathname,
      )
      ? url.pathname + url.search + url.hash
      : fallback
  } catch {
    return fallback
  }
}
export function errorText(error: unknown): string {
  const detail = readApiError(error)
  if (detail.code === 'STEP_UP_CANCELLED' || detail.code === 'ERR_CANCELED' || detail.name === 'AbortError') return ''
  return typeof error === 'object' && error && 'message' in error
    ? String(error.message)
    : String(error)
}
export function display(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
// Editable contract decimals share one display format without Number rounding.
export function formatContractDecimal(value: string | null | undefined): string {
  if (!value || !/^\d+(?:\.\d*)?$/.test(value)) return value || ''
  const [integer, fraction = ''] = value.split('.')
  const tail = fraction.replace(/0+$/, '')
  return integer.replace(/^0+(?=\d)/, '') + (tail ? '.' + tail : '')
}
// Usage and settlement costs arrive as decimal strings. Keep their exact value
// while adding grouping; the native compact-number formatter loses precision
// and treats unknown currency values as zero.
export function formatUsageNumber(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'number') {
    return Number.isFinite(value)
      ? value.toLocaleString('en-US', { maximumFractionDigits: 2 })
      : '—'
  }
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(String(value))
  if (!match) return '—'
  return (
    match[1] +
    match[2].replace(/\B(?=(\d{3})+(?!\d))/g, ',') +
    (match[3] ? '.' + match[3] : '')
  )
}
export function formatUsageCost(value: unknown, decimals?: 2): string {
  if (value === null || value === undefined || value === '') return '—'
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(String(value))
  if (!match) return '—'
  if (decimals === 2) {
    const fraction = match[3] || ''
    const cents = BigInt(match[2]) * 100n + BigInt(fraction.slice(0, 2).padEnd(2, '0')) + (fraction[2] >= '5' ? 1n : 0n)
    return match[1] + '$' + formatUsageNumber(String(cents / 100n)) + '.' + String(cents % 100n).padStart(2, '0')
  }
  const fraction = (match[3] || '').replace(/0+$/, '').padEnd(2, '0')
  return match[1] + '$' + formatUsageNumber(match[2]) + '.' + fraction
}
// Derive the unclassified remainder from the server's costs, never model names.
// Align decimal places before subtraction so large totals and tiny costs stay exact.
export function otherModelCost(metrics?: Metrics | null): string | null {
  return subtractUsageCosts([metrics?.cost_usd, metrics?.opus_cost_usd, metrics?.fable_cost_usd])
}
export function nonFableCost(metrics?: Metrics | null): string | null {
  return subtractUsageCosts([metrics?.cost_usd, metrics?.fable_cost_usd])
}
function subtractUsageCosts(values: unknown[]): string | null {
  const parts = values.map((value) => typeof value === 'string' ? /^(\d+)(?:\.(\d+))?$/.exec(value) : null)
  if (!parts.every((part): part is RegExpExecArray => part !== null)) return null
  const scale = Math.max(...parts.map((part) => (part[2] || '').length))
  const amounts = parts.map((part) => BigInt(part[1] + (part[2] || '').padEnd(scale, '0')))
  const remainder = amounts.slice(1).reduce((total, amount) => total - amount, amounts[0])
  if (remainder < 0n) return null
  const digits = remainder.toString().padStart(scale + 1, '0')
  return scale ? digits.slice(0, -scale) + '.' + digits.slice(-scale) : digits
}
export function proxyAvailable(proxy: VendorProxy, now = Date.now()): boolean {
  return (
    proxy.status === 'active' &&
    (!proxy.expires_at || Date.parse(proxy.expires_at) > now) &&
    !['expired', 'inactive', 'disabled'].includes(proxy.effective_status || '')
  )
}
export function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = name
  anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
export function selectionForPage(
  current: Set<number>,
  page: number[],
  checked: boolean,
): Set<number> {
  const next = new Set(current)
  for (const id of page) checked ? next.add(id) : next.delete(id)
  return next
}
// A metadata whitelist is also enforced server-side; UI never serializes a live proxy DTO wholesale.
export function proxyMetadata(proxy: VendorProxy) {
  return {
    name: proxy.name,
    protocol: proxy.protocol,
    host: proxy.host,
    port: proxy.port,
    vendor_notes: proxy.vendor_notes,
    region_label_country_code: proxy.region_label_country_code,
    region_label_city: proxy.region_label_city,
    expires_at: proxy.expires_at,
  }
}
export async function readImport(
  file: File,
  limitMessage = 'Import exceeds 5 MiB',
  signal?: AbortSignal,
): Promise<string> {
  if (file.size > 5 * 1024 * 1024) throw new Error(limitMessage)
  if (!signal) return file.text()
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    const abort = () => { reader.abort(); reject(new DOMException('Import read cancelled', 'AbortError')) }
    reader.onloadend = () => signal.removeEventListener('abort', abort)
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
    reader.onerror = () => reject(reader.error || new Error('Import read failed'))
    if (signal.aborted) { abort(); return }
    signal.addEventListener('abort', abort, { once: true })
    reader.readAsText(file)
  })
}
export async function boundedQueue<T>(
  items: T[],
  signal: AbortSignal,
  execute: (item: T) => Promise<void>,
  concurrency = 2,
) {
  let index = 0
  await Promise.all(
    Array.from({ length: Math.min(concurrency, items.length) }, async () => {
      while (!signal.aborted && index < items.length) {
        const item = items[index++]
        await execute(item)
      }
    }),
  )
}
export function dateRange(
  days: number,
  now = new Date(),
): { start_date: string; end_date: string } {
  const format = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  const start = new Date(now)
  start.setDate(start.getDate() - days + 1)
  return { start_date: format(start), end_date: format(now) }
}

export function proxyProbeOutcome(response: Record<string, unknown>) {
  const status = ['success', 'failed', 'skipped'].includes(
    String(response.status),
  )
    ? String(response.status)
    : 'failed'
  const test = response.test as
    | { latency_ms?: number; message?: string }
    | undefined
  const report = response.quality as ProxyQuality | undefined
  return {
    status,
    detail: [
      report?.grade,
      report?.score,
      report?.base_latency_ms ?? test?.latency_ms,
      test?.message,
    ]
      .filter((x) => x != null)
      .map(String)
      .join(' · '),
    report,
  }
}
export function accountQuotaRows(quota?: Record<string, unknown>) {
  return ['five_hour', 'seven_day', 'seven_day_fable'].flatMap((name) => {
    const value = quota?.[name]
    return value && typeof value === 'object'
      ? ([{ name, ...(value as Record<string, unknown>) }] as Record<
        string,
        unknown
      >[])
      : []
  })
}

export function toLocalDateTime(value = new Date().toISOString()): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16)
}

export function displayPercent(value: number | null | undefined): string {
  return value == null || !Number.isFinite(value)
    ? '—'
    : (value * 100).toFixed(1) + '%'
}

// Statement timestamps bound a half-open interval in the statement's timezone.
export function statementPeriod(
  startAt: string | undefined,
  endAt: string | undefined,
  timezone: string | undefined,
): string {
  if (!startAt || !endAt || !timezone) return '—'
  const start = Date.parse(startAt)
  const end = Date.parse(endAt)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start)
    return '—'
  try {
    const formatter = new Intl.DateTimeFormat('en', {
      timeZone: timezone,
      calendar: 'gregory',
      numberingSystem: 'latn',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    })
    const date = (timestamp: number) => {
      const parts = formatter.formatToParts(timestamp)
      return ['year', 'month', 'day']
        .map((type) => parts.find((part) => part.type === type)?.value)
        .join('-')
    }
    return `${date(start)} — ${date(end - 1)}`
  } catch {
    return '—'
  }
}
