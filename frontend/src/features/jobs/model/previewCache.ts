// This cache owns only disposable previews. Keep the persisted name, schema and key format.
const DB_NAME = 'sub2api-batch-image-preview-cache'
const STORE_NAME = 'thumbnails'
const MAX_AGE_MS = 3 * 24 * 60 * 60 * 1000
const MAX_ENTRIES = 120
const MAX_BYTES = 48 * 1024 * 1024

interface PreviewCacheRecord {
  key: string
  blob: Blob
  size: number
  createdAt: number
  lastAccessedAt: number
}

export function previewCacheKey(scope: string | null, apiKeyId: number, batchId: string, customId: string, imageIndex = 0): string {
  return [scope, apiKeyId, batchId, customId, imageIndex].map(part => encodeURIComponent(String(part))).join(':')
}

export interface PreviewCache {
  get(key: string, signal: AbortSignal): Promise<Blob | null>
  put(key: string, blob: Blob, signal: AbortSignal): Promise<void>
  remove(key: string, signal: AbortSignal): Promise<void>
  cleanup(signal?: AbortSignal): Promise<void>
  dispose(): void
}

function usable(record: PreviewCacheRecord, now: number): boolean {
  return record.blob instanceof Blob && Number.isFinite(record.createdAt)
    && Number.isFinite(record.lastAccessedAt) && Number.isFinite(record.size) && record.size >= 0
    && now - record.createdAt <= MAX_AGE_MS
}

export function createPreviewCache(): PreviewCache {
  const lifetime = new AbortController()
  let database: Promise<IDBDatabase | null> | null = null
  const transactions = new Set<IDBTransaction>()

  function open(signal?: AbortSignal): Promise<IDBDatabase | null> {
    if (lifetime.signal.aborted || signal?.aborted) return Promise.resolve(null)
    if (!database) {
      database = new Promise(resolve => {
        let settled = false
        const finish = (db: IDBDatabase | null) => { settled = true; resolve(db) }
        try {
          // The property getter itself can throw in a restricted browser context.
          if (typeof window === 'undefined' || !window.indexedDB) { finish(null); return }
          const request = window.indexedDB.open(DB_NAME, 1)
          request.onupgradeneeded = () => {
            try {
              const db = request.result
              if (!db.objectStoreNames.contains(STORE_NAME)) {
                const store = db.createObjectStore(STORE_NAME, { keyPath: 'key' })
                store.createIndex('lastAccessedAt', 'lastAccessedAt', { unique: false })
              }
            } catch {
              request.transaction?.abort()
              finish(null)
            }
          }
          request.onsuccess = () => {
            const db = request.result
            if (lifetime.signal.aborted || settled) { db.close(); finish(null); return }
            db.onversionchange = () => { db.close(); database = null }
            finish(db)
          }
          request.onerror = () => finish(null)
          request.onblocked = () => finish(null)
        } catch {
          // Browser storage may be unavailable; previews can still use the content API.
          finish(null)
        }
      })
    }
    const pending = database
    return new Promise(resolve => {
      const finish = (db: IDBDatabase | null) => {
        signal?.removeEventListener('abort', abort)
        lifetime.signal.removeEventListener('abort', abort)
        resolve(db)
      }
      const abort = () => finish(null)
      signal?.addEventListener('abort', abort, { once: true })
      lifetime.signal.addEventListener('abort', abort, { once: true })
      // IDB open cannot be aborted; stop this wait and close any late connection on dispose.
      void pending.then(finish)
    })
  }

  async function transact<T>(fallback: T, enqueue: (store: IDBObjectStore, result: (value: T) => void, abort: () => void) => void, signal?: AbortSignal): Promise<T> {
    const db = await open(signal)
    if (!db || lifetime.signal.aborted || signal?.aborted) return fallback
    return new Promise(resolve => {
      let tx: IDBTransaction
      try { tx = db.transaction(STORE_NAME, 'readwrite') } catch { resolve(fallback); return }
      transactions.add(tx)
      let result = fallback
      const abort = () => {
        try { tx.abort() } catch { /* Already completed or aborted. */ }
      }
      const finish = (value: T) => {
        transactions.delete(tx)
        signal?.removeEventListener('abort', abort)
        resolve(value)
      }
      tx.oncomplete = () => finish(result)
      tx.onabort = () => finish(fallback)
      tx.onerror = () => { /* The transaction abort event resolves the optional cache miss. */ }
      signal?.addEventListener('abort', abort, { once: true })
      try { enqueue(tx.objectStore(STORE_NAME), value => { result = value }, abort) } catch { abort() }
    })
  }

  async function get(key: string, signal: AbortSignal): Promise<Blob | null> {
    return transact<Blob | null>(null, (store, result, abort) => {
      const request: IDBRequest<PreviewCacheRecord | undefined> = store.get(key)
      request.onsuccess = () => {
        try {
          const record = request.result
          if (!record) return
          const now = Date.now()
          if (!usable(record, now)) { store.delete(key); return }
          // Touch in the same transaction: a late touch cannot resurrect an evicted record.
          store.put({ ...record, lastAccessedAt: now })
          result(record.blob)
        } catch { abort() }
      }
    }, signal)
  }

  async function cleanup(signal?: AbortSignal): Promise<void> {
    await transact(undefined, (store, _result, abort) => {
      const request: IDBRequest<PreviewCacheRecord[]> = store.getAll()
      request.onsuccess = () => {
        try {
          const now = Date.now()
          const kept: PreviewCacheRecord[] = []
          let totalBytes = 0
          for (const record of request.result) {
            if (!usable(record, now)) { store.delete(record.key); continue }
            kept.push(record)
            totalBytes += record.size || record.blob.size
          }
          kept.sort((a, b) => a.lastAccessedAt - b.lastAccessedAt)
          let count = kept.length
          for (const record of kept) {
            if (count <= MAX_ENTRIES && totalBytes <= MAX_BYTES) break
            store.delete(record.key)
            totalBytes -= record.size || record.blob.size
            count -= 1
          }
        } catch { abort() }
      }
    }, signal)
  }

  async function put(key: string, blob: Blob, signal: AbortSignal): Promise<void> {
    await transact(undefined, store => {
      const now = Date.now()
      const record: PreviewCacheRecord = { key, blob, size: blob.size, createdAt: now, lastAccessedAt: now }
      store.put(record)
    }, signal)
    await cleanup(signal)
  }

  async function remove(key: string, signal: AbortSignal): Promise<void> {
    await transact(undefined, store => { store.delete(key) }, signal)
  }

  function dispose(): void {
    if (lifetime.signal.aborted) return
    lifetime.abort()
    for (const tx of transactions) {
      try { tx.abort() } catch { /* A completed transaction may still have its event queued. */ }
    }
    transactions.clear()
    void database?.then(db => db?.close())
  }

  return { get, put, remove, cleanup, dispose }
}
