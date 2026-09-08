export interface CodexModelsManifestResult {
  content: string
  modelCount: number
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

  const payload: unknown = await response.json()
  if (!isCodexModelsManifest(payload)) {
    throw new Error('Codex models response is not a valid manifest')
  }

  return {
    content: JSON.stringify(payload, null, 2),
    modelCount: payload.models.length
  }
}
