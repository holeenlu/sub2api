export interface CodexModelsManifestResult {
  content: string
  modelCount: number
  responseBytes: number
}

function normalizeCodexApiRoot(baseUrl: string): string {
  const fallback = typeof window !== 'undefined' ? window.location.origin : ''
  const value = (baseUrl || fallback).trim().replace(/\/+$/, '')
  return value.replace(/\/v1$/i, '')
}

export function buildCodexModelsManifestUrl(baseUrl: string): string {
  const apiRoot = normalizeCodexApiRoot(baseUrl)
  return `${apiRoot}/backend-api/codex/models`
}

export function buildCodexModelCatalogUrl(baseUrl: string): string {
  return `${normalizeCodexApiRoot(baseUrl)}/v1/models`
}

function isCodexModelsManifest(value: unknown): value is { models: unknown[] } {
  return typeof value === 'object' && value !== null && Array.isArray((value as { models?: unknown }).models)
}

export async function fetchCodexModelsManifest(
  baseUrl: string,
  apiKey: string,
  signal?: AbortSignal
): Promise<CodexModelsManifestResult> {
  const response = await fetch(buildCodexModelsManifestUrl(baseUrl), {
    method: 'GET',
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${apiKey}`
    },
    cache: 'no-store',
    signal
  })

  if (!response.ok) {
    throw new Error(`Codex models request failed with status ${response.status}`)
  }

  const text = await response.text()
  const payload: unknown = JSON.parse(text)
  if (!isCodexModelsManifest(payload)) {
    throw new Error('Codex models response is not a valid manifest')
  }

  return {
    content: JSON.stringify(payload, null, 2),
    modelCount: payload.models.length,
    responseBytes: new TextEncoder().encode(text).byteLength
  }
}
