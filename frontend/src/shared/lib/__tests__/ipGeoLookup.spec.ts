import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { createIpGeoLookup, formatGeoLabel, IP_GEO_CACHE_LIMIT, isPrivateIp, type IpGeoLookup } from '../ipGeoLookup'

const instances: IpGeoLookup[] = []
const legacyKey = 'sub2api:ip-geo-cache:v1'
let storage: { getItem: ReturnType<typeof vi.fn>; setItem: ReturnType<typeof vi.fn>; removeItem: ReturnType<typeof vi.fn>; clear: ReturnType<typeof vi.fn> }
function setup(initial: string | null = 'user7/session-a') {
  const scope = ref<string | null>(initial)
  const requestJson = vi.fn<(url: string, signal: AbortSignal) => Promise<unknown>>()
  const lookup = createIpGeoLookup({ scope, requestJson })
  instances.push(lookup)
  return { scope, requestJson, lookup }
}
function deferred<T = unknown>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const response = (ip: string) => ({ ip, country_code: 'US', region: 'California', city: 'Mountain View' })
const resolveBatch = async (url: string) => new URL(url).searchParams.get('ip')!.split(',').map(response)

beforeEach(() => {
  storage = { getItem: vi.fn(), setItem: vi.fn(), removeItem: vi.fn(), clear: vi.fn() }
  vi.stubGlobal('localStorage', storage)
})
afterEach(() => {
  for (const instance of instances.splice(0)) instance.dispose()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('IP geolocation initialization and original display rules', () => {
  it('does not touch storage or issue requests at module import', async () => {
    vi.resetModules()
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await import('../ipGeoLookup')
    expect(storage.getItem).not.toHaveBeenCalled()
    expect(storage.removeItem).not.toHaveBeenCalled()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('deletes only the verified old index during explicit initialization and never persists IPs', async () => {
    const { lookup, requestJson } = setup()
    expect(storage.removeItem.mock.calls).toEqual([[legacyKey]])
    expect(storage.getItem).not.toHaveBeenCalled()
    expect(storage.clear).not.toHaveBeenCalled()
    expect(requestJson).not.toHaveBeenCalled()
    requestJson.mockResolvedValue(response('8.8.8.8'))
    await lookup.fetchOne('8.8.8.8')
    expect(storage.setItem).not.toHaveBeenCalled()
  })

  it('keeps in-memory lookups usable when browser storage is unavailable', async () => {
    storage.removeItem.mockImplementation(() => { throw new Error('Storage disabled') })
    const { lookup, requestJson } = setup()
    requestJson.mockResolvedValue(response('8.8.8.8'))
    await lookup.fetchOne('8.8.8.8')
    expect(lookup.getEntry('8.8.8.8').status).toBe('success')
  })

  it.each(['10.0.0.1', '127.0.0.1', '192.168.1.1', '172.16.0.1', '172.31.255.255', '169.254.1.1', '::1', 'fe80::1', 'fe90::1', 'febf::1', 'fc00::1', 'fd00::1', 'fdff::1'])('filters the existing private/reserved address %s', async ip => {
    const { lookup, requestJson } = setup()
    expect(isPrivateIp(ip)).toBe(true)
    await lookup.fetchOne(ip)
    expect(lookup.getEntry(ip)).toEqual({ status: 'private' })
    expect(requestJson).not.toHaveBeenCalled()
  })

  it.each(['8.8.8.8', '172.32.0.1', '121.35.47.43', 'fec0::1', 'fbff::1', 'fe7f::1'])('preserves the existing non-private classification for %s', ip => {
    expect(isPrivateIp(ip)).toBe(false)
  })

  it('formats location labels without empty fields', () => {
    expect(formatGeoLabel({ countryCode: 'CN', region: 'Guangdong', city: 'Shenzhen' })).toBe('CN · Guangdong · Shenzhen')
    expect(formatGeoLabel({ countryCode: 'US', city: '' })).toBe('US')
  })
})

describe('scoped IP queries', () => {
  it('keeps requests manual, preserves the GeoJS URL, details and forced refresh', async () => {
    const { lookup, requestJson } = setup()
    expect(lookup.getEntry('8.8.8.8')).toEqual({ status: 'idle' })
    expect(requestJson).not.toHaveBeenCalled()
    requestJson.mockResolvedValue({ ...response('8.8.8.8'), organization: 'Example ISP', timezone: 'America/Los_Angeles', accuracy: 10, latitude: '37.4', longitude: '-122.1' })
    await lookup.fetchOne('8.8.8.8')
    expect(requestJson).toHaveBeenCalledWith('https://get.geojs.io/v1/ip/geo/8.8.8.8.json', expect.any(AbortSignal))
    expect(lookup.getEntry('8.8.8.8')).toMatchObject({ status: 'success', label: 'US · California · Mountain View', detail: { organization: 'Example ISP', accuracy: 10 } })
    await lookup.fetchOne('8.8.8.8')
    expect(requestJson).toHaveBeenCalledTimes(1)
    await lookup.fetchOne('8.8.8.8', true)
    expect(requestJson).toHaveBeenCalledTimes(2)
  })

  it('expires successes only after the original 24-hour TTL', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-25T00:00:00Z'))
    const { lookup, requestJson } = setup()
    requestJson.mockResolvedValue(response('8.8.8.8'))
    await lookup.fetchOne('8.8.8.8')
    vi.setSystemTime(new Date('2026-10-26T00:00:00Z'))
    expect(lookup.getEntry('8.8.8.8').status).toBe('success')
    vi.advanceTimersByTime(1)
    expect(lookup.getEntry('8.8.8.8').status).toBe('idle')
    await lookup.fetchOne('8.8.8.8')
    expect(requestJson).toHaveBeenCalledTimes(2)
  })

  it.each([null, {}, [], { ip: '8.8.8.8' }, { ip: '8.8.8.8', country_code: 7 }, { ip: '8.8.8.8', country_code: ' ' }, { ip: '1.1.1.1', country_code: 'US' }, { ip: '8.8.8.8', country_code: 'US', city: {} }])('marks malformed single responses as error: %j', async raw => {
    const { lookup, requestJson } = setup()
    requestJson.mockResolvedValue(raw)
    await lookup.fetchOne('8.8.8.8')
    expect(lookup.getEntry('8.8.8.8').status).toBe('error')
  })

  it('marks a transport rejection as error and permits manual retry', async () => {
    const { lookup, requestJson } = setup()
    requestJson.mockRejectedValueOnce(new Error('Offline')).mockResolvedValueOnce(response('8.8.8.8'))
    await lookup.fetchOne('8.8.8.8')
    expect(lookup.getEntry('8.8.8.8').status).toBe('error')
    await lookup.fetchOne('8.8.8.8')
    expect(lookup.getEntry('8.8.8.8').status).toBe('success')
  })

  it('does not share cache entries between separately created instances', async () => {
    const first = setup(); const second = setup()
    first.requestJson.mockResolvedValue(response('8.8.8.8'))
    await first.lookup.fetchOne('8.8.8.8')
    expect(second.lookup.getEntry('8.8.8.8').status).toBe('idle')
  })

  it('keeps a forced refresh result when the superseded request finishes later', async () => {
    const old = deferred()
    const { lookup, requestJson } = setup()
    requestJson.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ ...response('8.8.8.8'), city: 'Fresh' })
    const first = lookup.fetchOne('8.8.8.8')
    await lookup.fetchOne('8.8.8.8', true)
    expect(requestJson.mock.calls[0][1].aborted).toBe(true)
    old.resolve({ ...response('8.8.8.8'), city: 'Old' })
    await first
    expect(lookup.getEntry('8.8.8.8').detail?.city).toBe('Fresh')
  })

  it.each(['user8/session-a', 'user7/session-b', null])('aborts and rejects late results when scope becomes %s', async next => {
    const old = deferred()
    const { lookup, requestJson, scope } = setup()
    requestJson.mockReturnValueOnce(old.promise)
    const request = lookup.fetchOne('8.8.8.8')
    const signal = requestJson.mock.calls[0][1]
    scope.value = next
    expect(signal.aborted).toBe(true)
    await request
    expect(lookup.getEntry('8.8.8.8').status).toBe('idle')
    old.resolve(response('8.8.8.8'))
    await Promise.resolve()
    expect(lookup.getEntry('8.8.8.8').status).toBe('idle')
  })

  it('does not let old scope completion replace a fresh result for the same IP', async () => {
    const old = deferred()
    const { lookup, requestJson, scope } = setup()
    requestJson.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ ...response('8.8.8.8'), city: 'New scope' })
    const first = lookup.fetchOne('8.8.8.8')
    scope.value = 'user8/session-b'
    await lookup.fetchOne('8.8.8.8')
    old.resolve({ ...response('8.8.8.8'), city: 'Old scope' })
    await first
    expect(lookup.getEntry('8.8.8.8').detail?.city).toBe('New scope')
  })

  it('aborts on effect-scope disposal and never restarts a disposed instance', async () => {
    const host = effectScope()
    const context = host.run(() => setup())!
    const pending = deferred()
    context.requestJson.mockReturnValueOnce(pending.promise)
    const request = context.lookup.fetchOne('8.8.8.8')
    host.stop()
    expect(context.requestJson.mock.calls[0][1].aborted).toBe(true)
    await request
    await context.lookup.fetchOne('1.1.1.1')
    expect(context.requestJson).toHaveBeenCalledTimes(1)
    pending.reject(new Error('Late transport failure'))
    await Promise.resolve()
    expect(context.lookup.getEntry('8.8.8.8').status).toBe('idle')
  })

  it('refuses requests without an active scope', async () => {
    const { lookup, requestJson } = setup(null)
    await lookup.fetchOne('8.8.8.8')
    await expect(lookup.fetchBatch(['8.8.8.8'])).resolves.toBe(false)
    expect(requestJson).not.toHaveBeenCalled()
  })
})

describe('bounded batch lookup', () => {
  it('deduplicates, filters private IPs and keeps exactly 50 IPs per GeoJS request', async () => {
    const { lookup, requestJson } = setup()
    requestJson.mockImplementation(resolveBatch)
    const ips = Array.from({ length: 61 }, (_, i) => '203.0.113.' + (i + 1))
    await expect(lookup.fetchBatch([...ips, ips[0], '10.0.0.5'])).resolves.toBe(true)
    expect(requestJson).toHaveBeenCalledTimes(2)
    expect(requestJson.mock.calls.map(([url]) => new URL(url).searchParams.get('ip')!.split(',').length)).toEqual([50, 11])
    expect(requestJson.mock.calls[0][0]).toMatch(/^https:\/\/get\.geojs\.io\/v1\/ip\/geo\.json\?ip=/)
    expect(lookup.getEntry('10.0.0.5').status).toBe('private')
    await lookup.fetchBatch(ips)
    expect(requestJson).toHaveBeenCalledTimes(2)
  })

  it.each([null, {}, [], [null], [{ ip: '8.8.8.8', country_code: 1 }]])('returns false and error entries for a malformed batch: %j', async raw => {
    const { lookup, requestJson } = setup()
    requestJson.mockResolvedValue(raw)
    await expect(lookup.fetchBatch(['8.8.8.8'])).resolves.toBe(false)
    expect(lookup.getEntry('8.8.8.8').status).toBe('error')
  })

  it('retains valid rows but reports missing rows as failed instead of empty success', async () => {
    const { lookup, requestJson } = setup()
    requestJson.mockResolvedValue([response('8.8.8.8')])
    await expect(lookup.fetchBatch(['8.8.8.8', '1.1.1.1'])).resolves.toBe(false)
    expect(lookup.getEntry('8.8.8.8').status).toBe('success')
    expect(lookup.getEntry('1.1.1.1').status).toBe('error')
  })

  it.each(['scope', 'caller', 'dispose'])('stops remaining chunks after %s cancellation and ignores late data', async cause => {
    const { lookup, requestJson, scope } = setup()
    const pending = deferred()
    const caller = new AbortController()
    requestJson.mockReturnValueOnce(pending.promise)
    const ips = Array.from({ length: 61 }, (_, i) => '203.0.113.' + (i + 1))
    const batch = lookup.fetchBatch(ips, { signal: caller.signal })
    if (cause === 'scope') scope.value = 'other-session'
    else if (cause === 'caller') caller.abort()
    else lookup.dispose()
    expect(requestJson.mock.calls[0][1].aborted).toBe(true)
    await expect(batch).resolves.toBe(false)
    pending.resolve(ips.slice(0, 50).map(response))
    await Promise.resolve()
    expect(requestJson).toHaveBeenCalledTimes(1)
    expect(lookup.getEntry(ips[0]).status).toBe('idle')
    expect(lookup.getEntry(ips[60]).status).toBe('idle')
  })

  it('bounds retained IP entries to 1024 and evicts the oldest without persisting an index', async () => {
    const { lookup, requestJson } = setup()
    requestJson.mockImplementation(resolveBatch)
    const ips = Array.from({ length: IP_GEO_CACHE_LIMIT + 1 }, (_, i) => '11.1.' + Math.floor(i / 250) + '.' + (i % 250 + 1))
    await expect(lookup.fetchBatch(ips)).resolves.toBe(true)
    expect(ips.filter(ip => lookup.getEntry(ip).status === 'success')).toHaveLength(IP_GEO_CACHE_LIMIT)
    expect(lookup.getEntry(ips[0]).status).toBe('idle')
    expect(lookup.getEntry(ips[ips.length - 1]).status).toBe('success')
    expect(storage.setItem).not.toHaveBeenCalled()
  })

  it('bounds abandoned in-flight owners and aborts the oldest request at capacity', async () => {
    const { lookup, requestJson } = setup()
    const pending = deferred()
    requestJson.mockReturnValue(pending.promise)
    const ips = Array.from({ length: IP_GEO_CACHE_LIMIT + 1 }, (_, i) => '11.2.' + Math.floor(i / 250) + '.' + (i % 250 + 1))
    const requests = ips.map(ip => lookup.fetchOne(ip))
    expect(requestJson.mock.calls[0][1].aborted).toBe(true)
    expect(ips.filter(ip => lookup.getEntry(ip).status === 'loading')).toHaveLength(IP_GEO_CACHE_LIMIT)
    lookup.dispose()
    await Promise.all(requests)
    expect(requestJson.mock.calls.every(([, signal]) => signal.aborted)).toBe(true)
    pending.reject(new Error('Late abandoned transport failure'))
    await Promise.resolve()
  })

  it('keeps later valid chunks but returns false after a current network failure', async () => {
    const { lookup, requestJson } = setup()
    requestJson.mockImplementation(resolveBatch).mockRejectedValueOnce(new Error('First chunk offline'))
    const ips = Array.from({ length: 61 }, (_, i) => '203.0.113.' + (i + 1))
    await expect(lookup.fetchBatch(ips)).resolves.toBe(false)
    expect(requestJson).toHaveBeenCalledTimes(2)
    expect(lookup.getEntry(ips[0]).status).toBe('error')
    expect(lookup.getEntry(ips[60]).status).toBe('success')
  })
})
