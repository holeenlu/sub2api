import { describe, expect, it } from 'vitest'
import { applyAccountModelReferences, readAccountModelReferences } from '../accountModelReferences'
import { projectBulkCredentials } from '../bulkCredentialPatch'
import { bulkTargetEligibility } from '../bulkTargetEligibility'

describe('independent managed account references', () => {
  it.each(['anthropic', 'openai'])('migrates legacy exact references and preserves real rewrites for %s', platform => {
    const parsed = readAccountModelReferences(platform, { seed: 'seed', alias: 'upstream' })
    const credentials: Record<string, unknown> = { refresh_token: 'preserved' }
    applyAccountModelReferences(credentials, platform, 'whitelist', parsed.allowedModels, parsed.modelMappings)
    expect(credentials).toEqual({ refresh_token: 'preserved', model_allowlist: ['seed'], model_mapping: { alias: 'upstream' } })
  })
  it.each([{ allowlist: [] }, { allowlist: null }])('treats saved inheritance $allowlist as authoritative over a stale same-name mapping', ({ allowlist }) => {
    expect(readAccountModelReferences('openai', { old: 'old', alias: 'upstream' }, allowlist)).toEqual({ allowedModels: [], modelMappings: [{ from: 'alias', to: 'upstream' }] })
  })
  it('loads explicit disabled/missing references without adding new catalog entries', () => {
    expect(readAccountModelReferences('anthropic', { old: 'old' }, ['disabled', 'missing']).allowedModels).toEqual(['disabled', 'missing'])
  })
  it.each(['grok', 'gemini', 'antigravity', 'kimi'])('retains %s legacy same-name mapping semantics', platform => {
    const credentials: Record<string, unknown> = {}
    applyAccountModelReferences(credentials, platform, 'combined', ['legacy'], [{ from: 'alias', to: 'upstream' }])
    expect(credentials).toEqual({ model_mapping: { legacy: 'legacy', alias: 'upstream' } })
  })
  it('never encodes an identity mapping as an actual managed rewrite', () => {
    const credentials: Record<string, unknown> = { model_mapping: { old: 'old' } }
    applyAccountModelReferences(credentials, 'openai', 'mapping', [], [{ from: 'same', to: 'same' }, { from: 'alias', to: 'upstream' }])
    expect(credentials).toEqual({ model_allowlist: null, model_mapping: { alias: 'upstream' } })
  })
  it('bulk whitelist saves an independent reference without clearing existing real rewrites', () => {
    const target = bulkTargetEligibility(['anthropic', 'openai'], ['apikey'], () => false)
    const patch = projectBulkCredentials({ modelRestriction: { mode: 'whitelist', allowedModels: ['seed'], mappings: [] } }, { allowAuthorize: true, allowTransportEdit: true, allowRateEdit: true }, target, false)
    expect(patch).toEqual({ model_allowlist: ['seed'] })
    expect({ model_mapping: { alias: 'upstream' }, ...patch }).toEqual({ model_allowlist: ['seed'], model_mapping: { alias: 'upstream' } })
  })
  it('bulk rewrites do not clear an independently saved allowlist', () => {
    const target = bulkTargetEligibility(['openai'], ['oauth'], () => false)
    expect(projectBulkCredentials({ modelRestriction: { mode: 'mapping', allowedModels: [], mappings: [{ from: 'alias', to: 'upstream' }] } }, { allowAuthorize: true, allowTransportEdit: true, allowRateEdit: true }, target, false)).toEqual({ model_mapping: { alias: 'upstream' } })
  })
  it('rejects a mixed managed/legacy batch that cannot share one atomic credential patch', () => {
    const target = bulkTargetEligibility(['openai', 'grok'], ['apikey'], () => false)
    expect(() => projectBulkCredentials({ modelRestriction: { mode: 'whitelist', allowedModels: [], mappings: [] } }, { allowAuthorize: true, allowTransportEdit: true, allowRateEdit: true }, target, false)).toThrow('separately')
  })
})
