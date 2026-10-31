import { getCurrentScope, hasInjectionContext, inject, onScopeDispose, shallowReactive, watch, type InjectionKey, type Ref } from 'vue'

export type IpGeoStatus = 'idle' | 'loading' | 'success' | 'error' | 'private'

export interface IpGeoDetail {
  countryCode?: string
  region?: string
  city?: string
  organization?: string
  timezone?: string
  accuracy?: number
  latitude?: string
  longitude?: string
}

export interface IpGeoEntry {
  status: IpGeoStatus
  label?: string
  detail?: IpGeoDetail
  fetchedAt?: number
}

export const IP_GEO_CACHE_LIMIT = 1024
const CACHE_TTL_MS = 24 * 60 * 60 * 1000
const BATCH_CHUNK_SIZE = 50
const LEGACY_CACHE_KEY = 'sub2api:ip-geo-cache:v1'
const GEO_SINGLE_URL = 'https://get.geojs.io/v1/ip/geo'
const GEO_BATCH_URL = 'https://get.geojs.io/v1/ip/geo.json'
const IDLE_ENTRY: Readonly<IpGeoEntry> = Object.freeze({ status: 'idle' })

export interface IpGeoLookupOptions {
  /** App-owned logical session/portal/data scope; null disables private-list lookups. */
  scope: Readonly<Ref<string | null>>
  /** Return parsed GeoJS JSON; reject transport, HTTP and JSON-decoding failures. */
  requestJson(url: string, signal: AbortSignal): Promise<unknown>
}
export interface IpGeoRequestOptions { signal?: AbortSignal }
export interface IpGeoLookup {
  readonly scope: Readonly<Ref<string | null>>
  getEntry(ip: string): Readonly<IpGeoEntry>
  fetchOne(ip: string, force?: boolean, options?: IpGeoRequestOptions): Promise<void>
  fetchBatch(ips: string[], options?: IpGeoRequestOptions): Promise<boolean>
  dispose(): void
}
export const ipGeoLookupKey: InjectionKey<IpGeoLookup> = Symbol('ui.ipGeoLookup')
export function useIpGeoLookup(): IpGeoLookup {
  const lookup = hasInjectionContext() ? inject(ipGeoLookupKey, null) : null
  if (!lookup) throw new Error('IP geolocation lookup is not provided')
  return lookup
}

export function isPrivateIp(ip: string): boolean {
  const v4 = ip.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (v4) {
    const a = Number(v4[1])
    const b = Number(v4[2])
    if (a === 10) return true
    if (a === 127) return true
    if (a === 169 && b === 254) return true
    if (a === 172 && b >= 16 && b <= 31) return true
    if (a === 192 && b === 168) return true
    return false
  }
  const lower = ip.toLowerCase()
  if (lower === '::1') return true
  const firstSegment = lower.split(':', 1)[0]
  if (/^fe[89ab][0-9a-f]$/.test(firstSegment)) return true
  if (/^f[cd][0-9a-f]{2}$/.test(firstSegment)) return true
  return false
}

export function formatGeoLabel(detail: IpGeoDetail): string {
  const parts = [detail.countryCode, detail.region, detail.city].filter(
    (part): part is string => Boolean(part && part.trim())
  )
  return parts.join(' · ')
}

function parseResult(raw: unknown, ip: string): IpGeoEntry | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  const value = raw as Record<string, unknown>
  if (value.ip !== ip || typeof value.country_code !== 'string' || !value.country_code.trim()) return null
  const textFields = ['region', 'city', 'organization', 'timezone', 'latitude', 'longitude'] as const
  if (textFields.some(key => value[key] != null && typeof value[key] !== 'string')) return null
  if (value.accuracy != null && (typeof value.accuracy !== 'number' || !Number.isFinite(value.accuracy))) return null
  const detail: IpGeoDetail = { countryCode: value.country_code }
  for (const key of textFields) if (typeof value[key] === 'string') detail[key] = value[key]
  if (typeof value.accuracy === 'number') detail.accuracy = value.accuracy
  return { status: 'success', label: formatGeoLabel(detail), detail: Object.freeze(detail), fetchedAt: Date.now() }
}

interface RequestOwner {
  controller: AbortController
  scope: string
  epoch: number
  ips: Set<string>
  cleanup(): void
}

/** Create at app initialization. No IP cache is read or written to persistent storage. */
export function createIpGeoLookup(options: IpGeoLookupOptions): IpGeoLookup {
  // This one verified, obsolete temporary index is the only storage key touched.
  try { globalThis.localStorage?.removeItem(LEGACY_CACHE_KEY) } catch { /* Storage may be disabled; never hydrate it. */ }
  // FIFO eviction bounds browser memory, including private/error/loading entries.
  const cache = shallowReactive(new Map<string, Readonly<IpGeoEntry>>())
  const owners = new Map<string, RequestOwner>()
  const active = new Set<RequestOwner>()
  let epoch = 0
  let disposed = false

  const isCurrent = (owner: RequestOwner) => !disposed && owner.epoch === epoch
    && owner.scope === options.scope.value && !owner.controller.signal.aborted

  function release(owner: RequestOwner) {
    for (const ip of owner.ips) {
      if (owners.get(ip) !== owner) continue
      owners.delete(ip)
      if (cache.get(ip)?.status === 'loading') cache.delete(ip)
    }
    owner.ips.clear()
  }
  function put(ip: string, entry: IpGeoEntry) {
    cache.set(ip, Object.freeze(entry))
    while (cache.size > IP_GEO_CACHE_LIMIT) {
      const oldest = cache.keys().next().value!
      cache.delete(oldest)
      const owner = owners.get(oldest)
      if (owner) {
        owners.delete(oldest)
        owner.ips.delete(oldest)
        if (owner.ips.size === 0) owner.controller.abort()
      }
    }
  }
  function getEntry(ip: string): Readonly<IpGeoEntry> {
    if (disposed || options.scope.value === null) return IDLE_ENTRY
    const entry = cache.get(ip)
    if (entry?.status === 'success' && (entry.fetchedAt === undefined || Date.now() - entry.fetchedAt > CACHE_TTL_MS)) {
      cache.delete(ip)
      return IDLE_ENTRY
    }
    return entry ?? IDLE_ENTRY
  }
  function begin(signal?: AbortSignal): RequestOwner | null {
    if (disposed || options.scope.value === null || signal?.aborted) return null
    // Bound concurrent owners too; an abandoned request cannot grow an unbounded registry.
    if (active.size >= IP_GEO_CACHE_LIMIT) active.values().next().value!.controller.abort()
    const controller = new AbortController()
    const onCallerAbort = () => controller.abort()
    const owner: RequestOwner = { controller, scope: options.scope.value, epoch, ips: new Set(), cleanup: () => {
      signal?.removeEventListener('abort', onCallerAbort)
      controller.signal.removeEventListener('abort', onAbort)
      release(owner)
      active.delete(owner)
    } }
    const onAbort = () => owner.cleanup()
    controller.signal.addEventListener('abort', onAbort, { once: true })
    signal?.addEventListener('abort', onCallerAbort, { once: true })
    active.add(owner)
    return owner
  }
  function markLoading(ip: string, owner: RequestOwner) {
    const previous = owners.get(ip)
    if (previous && previous !== owner) {
      owners.delete(ip)
      previous.ips.delete(ip)
      if (previous.ips.size === 0) previous.controller.abort()
    }
    owners.set(ip, owner)
    owner.ips.add(ip)
    put(ip, { status: 'loading' })
  }
  function apply(owner: RequestOwner, ip: string, entry: IpGeoEntry) {
    if (isCurrent(owner) && owners.get(ip) === owner) put(ip, entry)
  }
  function request(url: string, signal: AbortSignal): Promise<unknown> {
    // Abort settles our caller even if a transport ignores its signal; late outcomes stay observed.
    return new Promise((resolve, reject) => {
      const onAbort = () => { signal.removeEventListener('abort', onAbort); reject(new DOMException('IP lookup canceled', 'AbortError')) }
      if (signal.aborted) { onAbort(); return }
      signal.addEventListener('abort', onAbort, { once: true })
      const finish = (callback: (value: unknown) => void, value: unknown) => {
        signal.removeEventListener('abort', onAbort)
        callback(value)
      }
      try {
        Promise.resolve(options.requestJson(url, signal)).then(value => finish(resolve, value), error => finish(reject, error))
      } catch (error) { finish(reject, error) }
    })
  }
  async function fetchOne(ip: string, force = false, requestOptions?: IpGeoRequestOptions): Promise<void> {
    if (disposed || options.scope.value === null || requestOptions?.signal?.aborted) return
    if (isPrivateIp(ip)) { put(ip, { status: 'private' }); return }
    const existing = getEntry(ip)
    if (!force && (existing.status === 'success' || existing.status === 'loading')) return
    const owner = begin(requestOptions?.signal)
    if (!owner) return
    markLoading(ip, owner)
    try {
      const raw = await request(GEO_SINGLE_URL + '/' + encodeURIComponent(ip) + '.json', owner.controller.signal)
      apply(owner, ip, parseResult(raw, ip) ?? { status: 'error' })
    } catch { apply(owner, ip, { status: 'error' }) }
    finally { owner.cleanup() }
  }
  async function fetchBatch(ips: string[], requestOptions?: IpGeoRequestOptions): Promise<boolean> {
    const owner = begin(requestOptions?.signal)
    if (!owner) return false
    let ok = true
    try {
      const targets = [...new Set(ips)].filter(ip => {
        if (isPrivateIp(ip)) { put(ip, { status: 'private' }); return false }
        const entry = getEntry(ip)
        return entry.status !== 'success' && entry.status !== 'loading'
      })
      for (let i = 0; i < targets.length; i += BATCH_CHUNK_SIZE) {
        if (!isCurrent(owner)) return false
        const chunk = targets.slice(i, i + BATCH_CHUNK_SIZE)
        chunk.forEach(ip => markLoading(ip, owner))
        try {
          const raw = await request(GEO_BATCH_URL + '?ip=' + chunk.map(encodeURIComponent).join(','), owner.controller.signal)
          if (!isCurrent(owner)) return false
          if (!Array.isArray(raw)) throw new Error('Invalid IP geolocation batch response')
          const byIp = new Map<string, unknown>()
          for (const item of raw) {
            if (!item || typeof item !== 'object' || Array.isArray(item) || typeof item.ip !== 'string') throw new Error('Invalid IP geolocation row')
            if (byIp.has(item.ip)) throw new Error('Duplicate IP geolocation row')
            byIp.set(item.ip, item)
          }
          for (const ip of chunk) {
            const entry = parseResult(byIp.get(ip), ip)
            if (!entry) ok = false
            apply(owner, ip, entry ?? { status: 'error' })
          }
        } catch {
          if (!isCurrent(owner)) return false
          ok = false
          chunk.forEach(ip => apply(owner, ip, { status: 'error' }))
        } finally { release(owner) }
      }
      return isCurrent(owner) && ok
    } finally { owner.cleanup() }
  }
  function invalidate() {
    epoch++
    for (const owner of [...active]) owner.controller.abort()
    owners.clear()
    cache.clear()
  }
  const stop = watch(options.scope, invalidate, { flush: 'sync' })
  function dispose() {
    if (disposed) return
    disposed = true
    stop()
    invalidate()
  }
  if (getCurrentScope()) onScopeDispose(dispose)
  return { scope: options.scope, getEntry, fetchOne, fetchBatch, dispose }
}
