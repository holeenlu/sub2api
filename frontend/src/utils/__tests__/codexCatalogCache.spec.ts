import { webcrypto } from 'node:crypto'
import { TextEncoder } from 'node:util'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  codexCatalogCacheKey,
  readCodexCatalogCache,
  writeCodexCatalogCache
} from '@/utils/codexCatalogCache'

describe('codexCatalogCache', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('crypto', webcrypto)
    vi.stubGlobal('TextEncoder', TextEncoder)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  async function cacheKey(apiKey = 'sk-local-test-key') {
    return codexCatalogCacheKey('openai', 'https://gateway.example/v1', apiKey)
  }

  it('hashes key identities and normalizes equivalent catalog URLs', async () => {
    const key = await cacheKey()
    expect(key).toMatch(/^sub2api:codex-catalog:v1:[a-f0-9]{64}$/)
    expect(key).not.toContain('sk-local-test-key')
    expect(key).not.toContain('gateway.example')
    expect(await codexCatalogCacheKey('openai', 'https://gateway.example/v1/', 'sk-local-test-key')).toBe(key)
    expect(await codexCatalogCacheKey('openai', 'https://gateway.example/', 'sk-local-test-key')).toBe(key)
  })

  it('isolates stored catalogs by platform, base URL and API key', async () => {
    const keys = await Promise.all([
      cacheKey(),
      codexCatalogCacheKey('composite', 'https://gateway.example/v1', 'sk-local-test-key'),
      codexCatalogCacheKey('openai', 'https://other.example/v1', 'sk-local-test-key'),
      cacheKey('sk-other-test-key')
    ])
    expect(new Set(keys).size).toBe(4)
    writeCodexCatalogCache(keys[0], { content: '{"models":[{"slug":"gpt-6-sol"}]}', modelCount: 1 })
    for (const key of keys.slice(1)) expect(readCodexCatalogCache(key)).toBeNull()
  })

  it('persists the exact last successful manifest and derives its count', async () => {
    const key = await cacheKey()
    const content = JSON.stringify({ models: [{ slug: 'gpt-6-sol' }, { slug: 'custom/model' }], custom: true }, null, 2)
    writeCodexCatalogCache(key, { content, modelCount: 999 })
    expect(readCodexCatalogCache(key)).toEqual({ content, modelCount: 2 })
    expect(localStorage.getItem(key!)).not.toContain('sk-local-test-key')
    expect(localStorage.length).toBe(1)
    // There is intentionally no freshness timeout for a saved fallback.
    vi.spyOn(Date, 'now').mockReturnValue(0)
    expect(readCodexCatalogCache(key)).toEqual({ content, modelCount: 2 })
    vi.restoreAllMocks()
  })

  it('saves an authoritative empty catalog over previously allowed models', async () => {
    const key = await cacheKey()
    writeCodexCatalogCache(key, { content: '{"models":[{"slug":"gpt-6-astra"}]}', modelCount: 1 })
    writeCodexCatalogCache(key, { content: '{"models":[]}', modelCount: 7 })
    expect(readCodexCatalogCache(key)).toEqual({ content: '{"models":[]}', modelCount: 0 })
  })

  it.each([
    '{invalid',
    'null',
    '{}',
    '{"version":2,"content":"{\"models\":[]}"}',
    '{"version":1,"content":"{\"models\":null}"}',
    '{"version":1,"content":"{}"}',
    '{"version":1,"content":{"models":[]}}'
  ])('ignores corrupt or incompatible saved content: %s', async (stored) => {
    const key = await cacheKey()
    localStorage.setItem(key!, stored)
    expect(readCodexCatalogCache(key)).toBeNull()
  })

  it('does not overwrite a good saved catalog with malformed input', async () => {
    const key = await cacheKey()
    const result = { content: '{"models":[{"slug":"gpt-6-sol"}]}', modelCount: 1 }
    writeCodexCatalogCache(key, result)
    writeCodexCatalogCache(key, { content: '{"error":"unavailable"}', modelCount: 0 })
    expect(readCodexCatalogCache(key)).toEqual(result)
  })

  it('does not persist un-hashed keys or missing cache identities', () => {
    for (const key of [null, 'sk-raw-api-key', 'sub2api:codex-catalog:v1:sk-raw-api-key']) {
      writeCodexCatalogCache(key, { content: '{"models":[]}', modelCount: 0 })
      expect(readCodexCatalogCache(key)).toBeNull()
    }
    expect(localStorage.length).toBe(0)
  })

  it('returns no identity if hashing is unavailable or fails', async () => {
    vi.stubGlobal('crypto', undefined)
    expect(await cacheKey()).toBeNull()
    vi.stubGlobal('crypto', { subtle: { digest: vi.fn().mockRejectedValue(new Error('unavailable')) } })
    expect(await cacheKey()).toBeNull()
    expect(await cacheKey('')).toBeNull()
  })

  it.each(['unavailable', 'denied', 'quota'])('tolerates %s local storage', async (kind) => {
    const key = await cacheKey()
    const rejectStorage = () => { throw new Error(kind) }
    vi.stubGlobal('localStorage', kind === 'unavailable' ? undefined : {
      getItem: rejectStorage,
      setItem: rejectStorage
    })
    expect(readCodexCatalogCache(key)).toBeNull()
    expect(() => writeCodexCatalogCache(key, { content: '{"models":[]}', modelCount: 0 })).not.toThrow()
  })
})
