import type { ManagedModel, ModelMappings, ModelPolicy } from '../api/managedModelTypes'

export type ManagedModelConflict =
  | { kind: 'policy'; baseline: ModelPolicy; local: ModelPolicy; server: ModelPolicy }
  | { kind: 'mappings'; baseline: ModelMappings; local: ModelMappings; server: ModelMappings }

type ConflictRow = { key: string; baseline: string; local: string; server: string; dirty: boolean }
const policyFields = ['catalog_mode', 'defaults', 'aliases'] as const
const commonModelFields = ['enabled', 'sort', 'display_name'] as const
const anthropicModelFields = ['capabilities', 'params', 'betas', 'aliases', 'kind'] as const
const owns = (value: object, key: string): boolean => Object.prototype.hasOwnProperty.call(value, key)
const keyOf = (...parts: string[]): string => JSON.stringify(parts)

function copy<T>(value: T): T {
  return value === undefined ? value : JSON.parse(JSON.stringify(value)) as T
}

// Object insertion order is not a draft change; array order remains significant.
function ordered(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(ordered)
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)
      .map(([key, item]) => [key, ordered(item)]))
  }
  return value
}

function display(value: unknown): string {
  return JSON.stringify(ordered(value)) ?? '—'
}

function row(key: string, baseline: unknown, local: unknown, server: unknown): ConflictRow {
  const before = display(baseline)
  const draft = display(local)
  return { key, baseline: before, local: draft, server: display(server), dirty: before !== draft }
}

function modelFields(platform: ModelPolicy['platform']): readonly (keyof ManagedModel)[] {
  return platform === 'anthropic' ? [...commonModelFields, ...anthropicModelFields] : commonModelFields
}

// Only editable values participate in model comparison, never upstream facts.
function editableModel(models: ModelPolicy['models'], id: string, platform: ModelPolicy['platform']): unknown {
  if (!owns(models, id)) return undefined
  const model = models[id]
  return Object.fromEntries(modelFields(platform).filter(field => owns(model, field)).map(field => [field, model[field]]))
}

function keys(...objects: object[]): string[] {
  return [...new Set(objects.flatMap(Object.keys))].sort()
}

export function modelConflictRows(conflict: ManagedModelConflict): ConflictRow[] {
  if (conflict.kind === 'mappings') {
    const { baseline, local, server } = conflict
    return keys(baseline.mappings, local.mappings, server.mappings).map(alias => row(
      keyOf('mappings', alias),
      owns(baseline.mappings, alias) ? baseline.mappings[alias] : undefined,
      owns(local.mappings, alias) ? local.mappings[alias] : undefined,
      owns(server.mappings, alias) ? server.mappings[alias] : undefined
    ))
  }
  // Keep the discriminant attached when narrowing the three snapshots.
  const policy = conflict
  return [
    ...policyFields.map(field => row(keyOf(field), policy.baseline[field], policy.local[field], policy.server[field])),
    ...keys(policy.baseline.models, policy.local.models, policy.server.models).map(id => row(
      keyOf('models', id),
      editableModel(policy.baseline.models, id, policy.server.platform),
      editableModel(policy.local.models, id, policy.server.platform),
      editableModel(policy.server.models, id, policy.server.platform)
    ))
  ]
}

export function mergeModelConflict(conflict: ManagedModelConflict, keepKeys: readonly string[]): ModelPolicy | ModelMappings {
  const requested = new Set(keepKeys)
  const selected = new Set(modelConflictRows(conflict).filter(item => item.dirty && requested.has(item.key)).map(item => item.key))
  if (conflict.kind === 'mappings') {
    const merged = copy(conflict.server)
    for (const alias of keys(conflict.baseline.mappings, conflict.local.mappings)) {
      if (!selected.has(keyOf('mappings', alias))) continue
      if (owns(conflict.local.mappings, alias)) {
        // Alias IDs are data, including names such as __proto__.
        Object.defineProperty(merged.mappings, alias, {
          value: conflict.local.mappings[alias], enumerable: true, writable: true, configurable: true
        })
      } else if (alias !== 'codex-auto-review') {
        delete merged.mappings[alias]
      }
    }
    return merged
  }
  const merged = copy(conflict.server)
  for (const field of policyFields) {
    if (!selected.has(keyOf(field))) continue
    if (field === 'aliases') {
      if (conflict.local.aliases === undefined) delete merged.aliases
      else merged.aliases = copy(conflict.local.aliases)
    } else if (field === 'defaults') merged.defaults = copy(conflict.local.defaults)
    else merged.catalog_mode = conflict.local.catalog_mode
  }
  for (const id of Object.keys(merged.models)) {
    if (!selected.has(keyOf('models', id)) || !owns(conflict.local.models, id)) continue
    const draft = editableModel(conflict.local.models, id, merged.platform) as Record<string, unknown>
    const target = merged.models[id]
    for (const field of modelFields(merged.platform)) {
      // Deleting an optional editable field is an explicit local change too.
      if (owns(draft, field)) Object.defineProperty(target, field, {
        value: copy(draft[field]), enumerable: true, writable: true, configurable: true
      })
      else Reflect.deleteProperty(target, field)
    }
  }
  return merged
}
