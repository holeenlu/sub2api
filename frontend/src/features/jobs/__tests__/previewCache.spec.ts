import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPreviewCache, previewCacheKey } from '../model/previewCache'

interface StoredPreview {
  key: string
  blob: Blob
  size: number
  createdAt: number
  lastAccessedAt: number
}
interface Request<T> {
  result?: T
  onsuccess?: () => void
}

// A controlled IDB transaction double, not a browser implementation. Writes commit together,
// transactions are serialized, and abort/error rolls them back to exercise the cache contract.
function storage({ autoOpen = true } = {}) {
  const records = new Map<string, StoredPreview>()
  const waiting: Array<() => void> = []
  let active = false
  let holdCommits = false
  let commit: (() => void) | undefined
  let failPut = false
  let throwOnPut = false
  const eventErrors: unknown[] = []
  function next() {
    active = false
    const start = waiting.shift()
    if (start) { active = true; queueMicrotask(start) }
  }
  const transaction = vi.fn(() => {
    let draft: Map<string, StoredPreview> | undefined
    const operations: Array<() => void> = []
    let finished = false
    const tx = {
      oncomplete: () => {}, onabort: () => {}, onerror: () => {},
      abort: vi.fn(() => {
        if (finished) return
        finished = true
        queueMicrotask(() => { tx.onabort(); if (draft) next() })
      }),
      objectStore: vi.fn(() => store),
    }
    function request<T>(operation: () => T): Request<T> {
      const result: Request<T> = {}
      operations.push(() => { result.result = operation(); if (!finished) result.onsuccess?.() })
      return result
    }
    const store = {
      get: (key: string) => request(() => draft?.get(key)),
      getAll: () => request(() => [...(draft?.values() || [])]),
      put: (record: StoredPreview) => {
        if (throwOnPut) throw new DOMException('Cannot clone preview', 'DataCloneError')
        return request(() => {
          if (failPut) { tx.onerror(); tx.abort(); return }
          draft?.set(record.key, { ...record })
        })
      },
      delete: (key: string) => request(() => { draft?.delete(key) }),
    }
    const start = () => {
      if (finished) { next(); return }
      draft = new Map(records)
      try {
        while (operations.length && !finished) operations.shift()?.()
      } catch (error) {
        // Like IDB, an uncaught request event exception aborts the transaction.
        eventErrors.push(error)
        tx.abort()
      }
      if (finished) return
      const finish = () => {
        if (finished) return
        finished = true
        records.clear()
        for (const [key, record] of draft || []) records.set(key, record)
        tx.oncomplete()
        next()
      }
      if (holdCommits) commit = finish
      else queueMicrotask(finish)
    }
    if (active) waiting.push(start)
    else { active = true; queueMicrotask(start) }
    return tx
  })
  const createIndex = vi.fn()
  const db = {
    close: vi.fn(), onversionchange: () => {}, transaction,
    objectStoreNames: { contains: () => false },
    createObjectStore: vi.fn(() => ({ createIndex })),
  }
  const opening = { result: db, onsuccess: () => {}, onerror: () => {}, onblocked: () => {}, onupgradeneeded: () => {} }
  const open = vi.fn(() => {
    if (autoOpen) queueMicrotask(() => { opening.onupgradeneeded(); opening.onsuccess() })
    return opening
  })
  vi.stubGlobal('indexedDB', { open })
  const cache = createPreviewCache()
  return {
    cache, records, open, opening, db, createIndex,
    hold: () => { holdCommits = true },
    commit: () => { holdCommits = false; commit?.() },
    failWrites: () => { failPut = true },
    throwOnWrites: () => { throwOnPut = true },
    eventErrors,
  }
}
const signal = () => new AbortController().signal
const blob = new Blob(['thumbnail'])
function entry(key: string, createdAt: number, lastAccessedAt = createdAt, size = blob.size): StoredPreview {
  return { key, blob, size, createdAt, lastAccessedAt }
}
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('preview cache policy and lifetime', () => {
  it('preserves the encoded scope/key/batch/item/index identity without delimiter collisions', () => {
    expect(previewCacheKey('session:user:scope', 7, 'batch/a', 'item:b')).toBe('session%3Auser%3Ascope:7:batch%2Fa:item%3Ab:0')
    const identities = [
      previewCacheKey('actor-a', 7, 'b', 'c'), previewCacheKey('actor-b', 7, 'b', 'c'),
      previewCacheKey('actor-a', 8, 'b', 'c'), previewCacheKey('actor-a', 7, 'child', 'c'),
      previewCacheKey('actor-a', 7, 'b', 'c', 1), previewCacheKey('actor-a', 7, 'b:c', 'd'),
      previewCacheKey('actor-a', 7, 'b', 'c:d'),
    ]
    expect(new Set(identities).size).toBe(identities.length)
  })

  it('retains the database/schema and reads only the exact scope key', async () => {
    const { cache, open, db, createIndex, records } = storage()
    const key = previewCacheKey('actor-a', 7, 'batch', 'item')
    await cache.put(key, blob, signal())
    expect(open).toHaveBeenCalledTimes(1)
    expect(open).toHaveBeenCalledWith('sub2api-batch-image-preview-cache', 1)
    expect(db.createObjectStore).toHaveBeenCalledWith('thumbnails', { keyPath: 'key' })
    expect(createIndex).toHaveBeenCalledWith('lastAccessedAt', 'lastAccessedAt', { unique: false })
    expect(await cache.get(key, signal())).toBe(blob)
    expect(await cache.get(previewCacheKey('actor-b', 7, 'batch', 'item'), signal())).toBeNull()
    expect(records.size).toBe(1)
    cache.dispose()
    await flushPromises()
    expect(db.close).toHaveBeenCalledTimes(1)
  })

  it('expires by creation time after three days even when recently touched, preserving the exact TTL boundary', async () => {
    const now = 2_000_000_000_000
    vi.spyOn(Date, 'now').mockReturnValue(now)
    const { cache, records } = storage()
    const ttl = 3 * 24 * 60 * 60 * 1000
    records.set('expired', entry('expired', now - ttl - 1, now))
    records.set('boundary', entry('boundary', now - ttl, now - 100))
    expect(await cache.get('expired', signal())).toBeNull()
    expect(records.has('expired')).toBe(false)
    expect(await cache.get('boundary', signal())).toBe(blob)
    expect(records.get('boundary')?.lastAccessedAt).toBe(now)
    expect(records.get('boundary')?.createdAt).toBe(now - ttl)
    cache.dispose()
  })

  it('keeps the most recently accessed 120 records and applies cleanup after insertion', async () => {
    const { cache, records } = storage()
    const now = Date.now()
    for (let i = 0; i < 120; i++) records.set(String(i), entry(String(i), now, i))
    await cache.get('0', signal())
    await cache.put('new', blob, signal())
    expect(records.size).toBe(120)
    expect(records.has('0')).toBe(true)
    expect(records.has('1')).toBe(false)
    expect(records.has('new')).toBe(true)
    cache.dispose()
  })

  it('enforces 48 MiB with LRU eviction and deletes expired records before counting bytes', async () => {
    const { cache, records } = storage()
    const now = Date.now()
    // Stored byte metadata lets us exercise the exact limit without allocating huge test images.
    const mb = 1024 * 1024
    records.set('expired', entry('expired', now - 4 * 24 * 60 * 60 * 1000, 0, 48 * mb))
    records.set('oldest', entry('oldest', now, 1, 1))
    records.set('keep-a', entry('keep-a', now, 2, 24 * mb))
    records.set('keep-b', entry('keep-b', now, 3, 24 * mb))
    await cache.cleanup()
    expect([...records.keys()]).toEqual(['keep-a', 'keep-b'])
    await cache.cleanup()
    expect(records.size).toBe(2)
    cache.dispose()
  })

  it('aborts an in-flight write on cancellation, without publishing uncommitted cache data', async () => {
    const { cache, records, hold, commit, db } = storage()
    hold()
    const controller = new AbortController()
    const writing = cache.put('cancelled', blob, controller.signal)
    await flushPromises()
    expect(records.size).toBe(0)
    controller.abort()
    await writing
    commit()
    expect(db.transaction.mock.results[0].value.abort).toHaveBeenCalledTimes(1)
    expect(records.size).toBe(0)
    cache.dispose()
  })

  it('waits for transaction commit and treats a quota failure as an optional cache miss', async () => {
    const { cache, records, hold, commit, failWrites } = storage()
    hold()
    const done = vi.fn()
    const writing = cache.put('new', blob, signal()).then(done)
    await flushPromises()
    expect(done).not.toHaveBeenCalled()
    commit()
    await writing
    expect(records.has('new')).toBe(true)
    failWrites()
    await expect(cache.put('quota', blob, signal())).resolves.toBeUndefined()
    expect(records.has('quota')).toBe(false)
    cache.dispose()
  })

  it('closes a late-opened database after dispose and never starts the abandoned operation', async () => {
    const { cache, opening, db } = storage({ autoOpen: false })
    const read = cache.get('late', signal())
    cache.dispose()
    cache.dispose()
    opening.onsuccess()
    expect(await read).toBeNull()
    expect(db.close).toHaveBeenCalledTimes(1)
    expect(db.transaction).not.toHaveBeenCalled()
  })

  it.each(['cancel', 'dispose'] as const)('settles an open wait on %s even before the browser answers', async action => {
    const { cache, opening, db } = storage({ autoOpen: false })
    const controller = new AbortController()
    const settled = vi.fn()
    const read = cache.get('late', controller.signal).then(settled)
    if (action === 'cancel') controller.abort()
    else cache.dispose()
    await flushPromises()
    const settledBeforeOpen = settled.mock.calls.length
    opening.onsuccess()
    await read
    cache.dispose()
    expect(settledBeforeOpen).toBe(1)
    expect(settled).toHaveBeenCalledWith(null)
    expect(db.transaction).not.toHaveBeenCalled()
  })

  it('evicts a broken preview without deleting another scope or batch', async () => {
    const { cache, records } = storage()
    await cache.put('broken', blob, signal())
    await cache.put('keep', blob, signal())
    await cache.remove('broken', signal())
    expect([...records.keys()]).toEqual(['keep'])
    cache.dispose()
  })

  it('degrades a synchronous failure in a cache-hit touch without an uncaught event error', async () => {
    const { cache, records, throwOnWrites, eventErrors } = storage()
    records.set('hit', entry('hit', Date.now()))
    throwOnWrites()
    expect(await cache.get('hit', signal())).toBeNull()
    expect(eventErrors).toEqual([])
    cache.dispose()
  })

  it('drops invalid cache timestamps instead of retaining previews beyond their TTL', async () => {
    const { cache, records } = storage()
    records.set('invalid', entry('invalid', NaN))
    expect(await cache.get('invalid', signal())).toBeNull()
    expect(records.has('invalid')).toBe(false)
    records.set('invalid', entry('invalid', NaN))
    await cache.cleanup()
    expect(records.size).toBe(0)
    cache.dispose()
  })

  it('aborts active transactions on dispose and rejects further reads', async () => {
    const { cache, hold, db, records } = storage()
    hold()
    const write = cache.put('late', blob, signal())
    await flushPromises()
    cache.dispose()
    await write
    expect(records.size).toBe(0)
    expect(db.transaction.mock.results[0].value.abort).toHaveBeenCalledTimes(1)
    expect(await cache.get('late', signal())).toBeNull()
  })

  it.each(['onblocked', 'onerror'] as const)('fails open as a cache miss on %s and closes any later connection', async event => {
    const { cache, opening, db } = storage({ autoOpen: false })
    const read = cache.get('late', signal())
    opening[event]()
    expect(await read).toBeNull()
    opening.onsuccess()
    expect(db.close).toHaveBeenCalledTimes(1)
    cache.dispose()
  })

  it('works without IndexedDB or when the browser refuses to open storage', async () => {
    vi.stubGlobal('indexedDB', undefined)
    const absent = createPreviewCache()
    expect(await absent.get('a', signal())).toBeNull()
    await absent.put('a', blob, signal())
    absent.dispose()
    vi.stubGlobal('indexedDB', { open: () => { throw new Error('storage disabled') } })
    const denied = createPreviewCache()
    expect(await denied.get('a', signal())).toBeNull()
    denied.dispose()
  })

  it('treats a restricted IndexedDB property getter as optional storage', async () => {
    vi.stubGlobal('indexedDB', undefined)
    Object.defineProperty(window, 'indexedDB', {
      configurable: true,
      get: () => { throw new DOMException('Storage disabled', 'SecurityError') },
    })
    const cache = createPreviewCache()
    await expect(cache.get('a', signal())).resolves.toBeNull()
    await expect(cache.put('a', blob, signal())).resolves.toBeUndefined()
    await expect(cache.cleanup()).resolves.toBeUndefined()
    cache.dispose()
  })
})
