import type { ApiKeyImportData } from '../api/keys'
export const MAX_KEY_IMPORT_BYTES = 5 * 1024 * 1024
export function parseKeyImport(text: string): ApiKeyImportData {
  const value: unknown = JSON.parse(text.replace(/^\uFEFF/, ''))
  if (typeof value !== 'object' || value === null || !('type' in value) || value.type !== 'sub2api-keys'
    || !('version' in value) || value.version !== 1 || !('keys' in value) || !Array.isArray(value.keys)
    || !value.keys.length || value.keys.length > 1000) throw new Error('Invalid Key import envelope')
  return { ...value, type: value.type, version: value.version, keys: value.keys }
}
