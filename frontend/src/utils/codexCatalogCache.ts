import type { CodexModelsManifestResult } from '@/api/codex'

const CACHE_PREFIX = 'sub2api:codex-catalog:v1:'
const CACHE_VERSION = 1

function validCacheKey(cacheKey: string | null): cacheKey is string {
  return cacheKey !== null && cacheKey.startsWith(CACHE_PREFIX) &&
    /^[a-f0-9]{64}$/.test(cacheKey.slice(CACHE_PREFIX.length))
}

function validatedManifest(content: unknown): CodexModelsManifestResult | null {
  if (typeof content !== 'string') return null
  try {
    const payload: unknown = JSON.parse(content)
    if (typeof payload !== 'object' || payload === null ||
        !('models' in payload) || !Array.isArray(payload.models)) return null
    return { content, modelCount: payload.models.length }
  } catch {
    return null
  }
}

export async function codexCatalogCacheKey(
  platform: string,
  baseUrl: string,
  apiKey: string
): Promise<string | null> {
  if (!platform.trim() || !apiKey.trim()) return null
  try {
    const subtle = globalThis.crypto?.subtle
    if (!subtle) return null
    // Match the model-catalog endpoint's base URL normalization. The key itself
    // must never be persisted, even when cryptographic hashing is unavailable.
    const fallback = typeof window !== 'undefined' ? window.location.origin : ''
    const apiRoot = (baseUrl || fallback).trim().replace(/\/+$/, '').replace(/\/v1$/i, '')
    const identity = JSON.stringify([platform.trim(), apiRoot, apiKey])
    const digest = await subtle.digest('SHA-256', new TextEncoder().encode(identity))
    const hash = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
    return `${CACHE_PREFIX}${hash}`
  } catch {
    return null
  }
}

export function readCodexCatalogCache(cacheKey: string | null): CodexModelsManifestResult | null {
  if (!validCacheKey(cacheKey)) return null
  try {
    const stored = globalThis.localStorage.getItem(cacheKey)
    if (!stored) return null
    const entry: unknown = JSON.parse(stored)
    if (typeof entry !== 'object' || entry === null ||
        !('version' in entry) || entry.version !== CACHE_VERSION || !('content' in entry)) return null
    return validatedManifest(entry.content)
  } catch {
    return null
  }
}

export function writeCodexCatalogCache(
  cacheKey: string | null,
  result: CodexModelsManifestResult
): void {
  if (!validCacheKey(cacheKey)) return
  const manifest = validatedManifest(result.content)
  if (!manifest) return
  try {
    // Keep the last successful catalog without expiry. A successful empty catalog
    // replaces old access just like a populated one; transient failures never do.
    globalThis.localStorage.setItem(cacheKey, JSON.stringify({
      version: CACHE_VERSION,
      content: manifest.content
    }))
  } catch {
    // Storage is optional: denied access or quota exhaustion must not block setup.
  }
}
