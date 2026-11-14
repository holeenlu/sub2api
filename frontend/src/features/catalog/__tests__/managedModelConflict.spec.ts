import { describe, expect, it } from 'vitest'
import type { ManagedModel, ModelMappings, ModelPolicy } from '../api/managedModelTypes'
import { mergeModelConflict, modelConflictRows, type ManagedModelConflict } from '../model/managedModelConflict'

const key = (...parts: string[]): string => JSON.stringify(parts)
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T

function model(id = 'example'): ManagedModel {
  return {
    id, display_name: id, kind: 'chat', enabled: true, sort: 1, aliases: ['old-alias'],
    capabilities: { vision: false },
    params: { max_tokens_default: 8192, max_tokens_cap: 16384, thinking_fallback_budget: 1024, on_adaptive: 'passthrough', on_enabled: 'strip' },
    betas: { allow_client: false }, family: 'server-family',
    catalog: { id, description: 'upstream facts', context_window: 100000 }, source_account_ids: [1]
  }
}

function policy(platform: ModelPolicy['platform'] = 'anthropic'): ModelPolicy {
  return {
    version: 0, platform, source: 'builtin', catalog_mode: 'policy_only', models: { example: model() },
    defaults: { enabled: true, max_tokens: 8192, thinking_fallback_budget: 1024, strip_context_1m: false, context_1m_whitelist: ['example'], normalize_thinking: true },
    aliases: { old: 'example' }
  }
}

function policyConflict(platform: ModelPolicy['platform'] = 'anthropic'): Extract<ManagedModelConflict, { kind: 'policy' }> {
  const baseline = policy(platform)
  return { kind: 'policy', baseline, local: clone(baseline), server: { ...clone(baseline), version: 12, source: 'upstream', updated_at: 'server-time' } }
}

function mappingConflict(): Extract<ManagedModelConflict, { kind: 'mappings' }> {
  const baseline: ModelMappings = { version: 0, mappings: { edit: 'old', remove: 'old', untouched: 'old', 'codex-auto-review': 'review-old' } }
  return { kind: 'mappings', baseline, local: clone(baseline), server: { ...clone(baseline), version: 23, updated_at: 'server-time' } }
}

function freeze(value: object): void {
  Object.values(value).forEach(item => { if (item && typeof item === 'object') freeze(item) })
  Object.freeze(value)
}

describe('managed model conflict comparison', () => {
  it('partitions policy rows and ignores version and authoritative model facts', () => {
    const conflict = policyConflict()
    conflict.local.version = 500
    conflict.local.source = 'local-source'
    conflict.local.updated_at = 'local-time'
    conflict.local.models.example.catalog = { id: 'forged', context_window: 999 }
    conflict.local.models.example.source_account_ids = [900]
    conflict.local.models.example.family = 'local-family'
    expect(modelConflictRows(conflict).map(row => [JSON.parse(row.key), row.dirty])).toEqual([
      [['catalog_mode'], false], [['defaults'], false], [['aliases'], false], [['models', 'example'], false]
    ])
  })

  it('compares JSON objects independent of insertion order and arrays as whole values', () => {
    const conflict = policyConflict()
    conflict.baseline.models.example.capabilities = { a: { x: 1, y: 2 }, b: false }
    conflict.local.models.example.capabilities = { b: false, a: { y: 2, x: 1 } }
    expect(modelConflictRows(conflict).find(row => row.key === key('models', 'example'))?.dirty).toBe(false)
    conflict.baseline.defaults.context_1m_whitelist = ['a', 'b']
    conflict.local.defaults.context_1m_whitelist = ['b', 'a']
    const defaults = modelConflictRows(conflict).find(row => row.key === key('defaults'))!
    expect(defaults.dirty).toBe(true)
    expect(JSON.parse(defaults.local).context_1m_whitelist).toEqual(['b', 'a'])
  })

  it('uses collision-free keys and an em dash for missing JSON values', () => {
    const conflict = policyConflict()
    conflict.local.models['a/b'] = model('a/b')
    conflict.server.models['a","b'] = model('a","b')
    delete conflict.local.aliases
    const rows = modelConflictRows(conflict)
    expect(new Set(rows.map(row => row.key)).size).toBe(rows.length)
    expect(rows.find(row => row.key === key('models', 'a/b'))).toMatchObject({ baseline: '—', server: '—', dirty: true })
    expect(rows.find(row => row.key === key('models', 'a","b'))).toMatchObject({ baseline: '—', local: '—', dirty: false })
    expect(rows.find(row => row.key === key('aliases'))?.local).toBe('—')
  })

  it('does not offer OpenAI read-only model fields as dirty changes', () => {
    const conflict = policyConflict('openai')
    conflict.local.models.example.params.max_tokens_cap = 99
    conflict.local.models.example.aliases = ['local']
    conflict.local.models.example.kind = 'image'
    conflict.local.models.example.capabilities = { changed: true }
    conflict.local.models.example.betas.allow_client = true
    expect(modelConflictRows(conflict).find(row => row.key === key('models', 'example'))?.dirty).toBe(false)
  })
})

describe('explicit policy conflict merge', () => {
  it('keeps seed version zero from the server and applies selected defaults as a whole partition', () => {
    const conflict = policyConflict()
    conflict.server.version = 0
    conflict.server.source = 'builtin'
    delete conflict.server.updated_at
    conflict.local.version = 9
    conflict.local.defaults.context_1m_whitelist = ['second', 'first']
    conflict.server.defaults.max_tokens = 32000
    const merged = mergeModelConflict(conflict, [key('defaults')]) as ModelPolicy
    expect(merged.defaults).toEqual(conflict.local.defaults)
    expect(merged.version).toBe(0)
    expect(merged.source).toBe('builtin')
    expect(merged).not.toHaveProperty('updated_at')
  })

  it('only keeps selected dirty partitions and always takes server identity and versions', () => {
    const conflict = policyConflict()
    conflict.local.catalog_mode = 'worker_only'
    conflict.local.aliases = { draft: 'example' }
    conflict.local.version = 999
    conflict.local.source = 'local'
    conflict.local.updated_at = 'local-time'
    conflict.server.defaults.max_tokens = 32000
    conflict.server.aliases = { remote: 'example' }
    const merged = mergeModelConflict(conflict, [key('catalog_mode'), key('defaults'), key('version'), 'unknown']) as ModelPolicy
    expect(merged.catalog_mode).toBe('worker_only')
    expect(merged.defaults).toEqual(conflict.server.defaults)
    expect(merged.aliases).toEqual(conflict.server.aliases)
    expect([merged.version, merged.source, merged.updated_at, merged.platform]).toEqual([12, 'upstream', 'server-time', 'anthropic'])
  })

  it('merges Anthropic editable fields while preserving server metadata and new models', () => {
    const conflict = policyConflict()
    const draft = conflict.local.models.example
    Object.assign(draft, { id: 'forged-id', enabled: false, sort: 7, display_name: 'Draft', kind: 'image', aliases: ['draft'], capabilities: { vision: true }, family: 'forged-family', catalog: { id: 'forged' }, source_account_ids: [777] })
    draft.params.max_tokens_cap = 24000
    draft.betas = { allow_client: true, pass_context_1m: false }
    conflict.server.models.example.catalog = { id: 'example', description: 'new facts', max_context_window: 200000 }
    conflict.server.models.example.source_account_ids = [2, 3]
    conflict.server.models.new = model('new')
    const merged = mergeModelConflict(conflict, [key('models', 'example'), key('models', 'new')]) as ModelPolicy
    expect(merged.models.example).toMatchObject({ id: 'example', enabled: false, sort: 7, display_name: 'Draft', kind: 'image', aliases: ['draft'], capabilities: { vision: true }, params: { max_tokens_cap: 24000 }, betas: { allow_client: true, pass_context_1m: false }, family: 'server-family' })
    expect(merged.models.example.catalog).toEqual(conflict.server.models.example.catalog)
    expect(merged.models.example.source_account_ids).toEqual([2, 3])
    expect(merged.models.new).toEqual(conflict.server.models.new)
  })

  it('only copies the three writable OpenAI fields even when the same row also has forged facts', () => {
    const conflict = policyConflict('openai')
    Object.assign(conflict.local.models.example, { enabled: false, sort: 4, display_name: 'Draft', kind: 'image', aliases: ['draft'], capabilities: { vision: true } })
    conflict.local.models.example.params.max_tokens_cap = 42
    conflict.local.models.example.betas.allow_client = true
    conflict.server.models.example.aliases = ['server']
    const merged = mergeModelConflict(conflict, [key('models', 'example')]) as ModelPolicy
    expect(merged.models.example).toEqual({ ...conflict.server.models.example, enabled: false, sort: 4, display_name: 'Draft' })
  })

  it('never resurrects a server-deleted model or deletes a model missing from the local draft', () => {
    const conflict = policyConflict()
    conflict.local.models.example.enabled = false
    delete conflict.server.models.example
    conflict.baseline.models.removedLocally = model('removedLocally')
    conflict.server.models.removedLocally = model('removedLocally')
    conflict.local.models.draftOnly = model('draftOnly')
    const merged = mergeModelConflict(conflict, [key('models', 'example'), key('models', 'removedLocally'), key('models', 'draftOnly')]) as ModelPolicy
    expect(Object.keys(merged.models)).toEqual(['removedLocally'])
    expect(merged.models.removedLocally).toEqual(conflict.server.models.removedLocally)
  })

  it('supports explicit optional field removal, preserves inputs and detaches every returned value', () => {
    const conflict = policyConflict()
    delete conflict.local.aliases
    delete conflict.local.models.example.capabilities
    const before = clone(conflict)
    freeze(conflict)
    const merged = mergeModelConflict(conflict, [key('aliases'), key('models', 'example')]) as ModelPolicy
    expect(merged).not.toHaveProperty('aliases')
    expect(merged.models.example).not.toHaveProperty('capabilities')
    merged.models.example.catalog!.context_window = 1
    merged.defaults.context_1m_whitelist.push('new')
    expect(conflict).toEqual(before)
  })
})

describe('explicit mapping conflict merge', () => {
  it('selects local additions, edits and deletions while preserving remote aliases and server version', () => {
    const conflict = mappingConflict()
    conflict.local.mappings.edit = 'local-edit'
    delete conflict.local.mappings.remove
    conflict.local.mappings.added = 'local-new'
    conflict.server.mappings.untouched = 'server-edit'
    conflict.server.mappings.remote = 'server-new'
    conflict.local.version = 500
    conflict.local.updated_at = 'local-time'
    const rows = modelConflictRows(conflict)
    expect(rows.find(row => row.key === key('mappings', 'added'))).toMatchObject({ baseline: '—', local: '"local-new"', server: '—', dirty: true })
    expect(rows.find(row => row.key === key('mappings', 'remove'))?.local).toBe('—')
    expect(rows.find(row => row.key === key('mappings', 'remote'))?.dirty).toBe(false)
    const merged = mergeModelConflict(conflict, ['edit', ...['edit', 'remove', 'added', 'untouched', 'remote'].map(alias => key('mappings', alias))]) as ModelMappings
    expect(merged).toEqual({ version: 23, updated_at: 'server-time', mappings: { edit: 'local-edit', added: 'local-new', untouched: 'server-edit', remote: 'server-new', 'codex-auto-review': 'review-old' } })
  })

  it('retains unselected server values and protects the review alias from deletion without fixing its target', () => {
    const conflict = mappingConflict()
    conflict.local.mappings.edit = 'unselected'
    delete conflict.local.mappings['codex-auto-review']
    conflict.server.mappings['codex-auto-review'] = 'server-review'
    const keep = [key('mappings', 'codex-auto-review')]
    const merged = mergeModelConflict(conflict, keep) as ModelMappings
    expect(merged.mappings.edit).toBe('old')
    expect(merged.mappings['codex-auto-review']).toBe('server-review')
    conflict.local.mappings['codex-auto-review'] = 'chosen-review'
    expect((mergeModelConflict(conflict, keep) as ModelMappings).mappings['codex-auto-review']).toBe('chosen-review')
  })

  it('handles unusual alias names as data and never mutates frozen snapshots', () => {
    const conflict = mappingConflict()
    Object.defineProperty(conflict.local.mappings, '__proto__', { value: 'model', enumerable: true })
    conflict.local.mappings['x/y'] = 'slash-model'
    const before = clone(conflict)
    freeze(conflict)
    const merged = mergeModelConflict(conflict, [key('mappings', '__proto__'), key('mappings', 'x/y')]) as ModelMappings
    expect(Object.prototype.hasOwnProperty.call(merged.mappings, '__proto__')).toBe(true)
    expect(merged.mappings['__proto__']).toBe('model')
    expect(Object.getPrototypeOf(merged.mappings)).toBe(Object.prototype)
    merged.mappings.edit = 'after-merge'
    expect(conflict).toEqual(before)
  })
})
