import { buildModelMappingObject, splitModelMappingObject, type ModelMappingEntry, type ModelRestrictionMode } from './modelRestrictions'

export const usesManagedModelReferences = (platform: string): boolean => platform === 'anthropic' || platform === 'openai'

export function readAccountModelReferences(platform: string, mapping?: unknown, allowlist?: unknown): { allowedModels: string[]; modelMappings: ModelMappingEntry[] } {
  const parsed = splitModelMappingObject(typeof mapping === 'object' && mapping !== null && !Array.isArray(mapping) ? Object.fromEntries(Object.entries(mapping)) : undefined)
  if (usesManagedModelReferences(platform) && (Array.isArray(allowlist) || allowlist === null)) {
    parsed.allowedModels = Array.isArray(allowlist) ? allowlist.filter((id): id is string => typeof id === 'string') : []
  }
  return parsed
}

/** Only the two migrated platforms separate references from actual rewrites. */
export function applyAccountModelReferences(credentials: Record<string, unknown>, platform: string, mode: ModelRestrictionMode, allowedModels: readonly string[], mappings: readonly ModelMappingEntry[]): void {
  const managed = usesManagedModelReferences(platform)
  const mapping = buildModelMappingObject(managed ? 'mapping' : mode, [...allowedModels], [...mappings])
  if (managed) {
    const ids = [...new Set(allowedModels.map(id => id.trim()).filter(Boolean))]
    credentials.model_allowlist = ids.length ? ids : null
    if (mapping) for (const [from, to] of Object.entries(mapping)) if (from === to) delete mapping[from]
  }
  if (mapping && Object.keys(mapping).length) credentials.model_mapping = mapping
  else delete credentials.model_mapping
}
