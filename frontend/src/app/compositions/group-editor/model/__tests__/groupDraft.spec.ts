import { describe, expect, it } from 'vitest'
import { buildCreateGroupRequest, buildUpdateGroupRequest, createGroupEditorDraft } from '../groupDraft'
const details = { routing: null, allowlist: { enabled: false, models: [] }, manifest: { enabled: false, account_ids: [], fallback_to_scheduler: false } }
describe('the existing atomic Group payload', () => {
 it('preserves zero prices/rates/RPM and uses the documented quota-unlimited representation', () => {
  const draft = createGroupEditorDraft(); draft.name = 'Zero'; draft.rate_multiplier = 0; draft.rpm_limit = 0; draft.daily_limit_usd = 0; draft.image_price_1k = 0
  const request = buildCreateGroupRequest(draft, details, false)
  expect(request).toMatchObject({ rate_multiplier: 0, rpm_limit: 0, daily_limit_usd: null, image_price_1k: 0 })
  expect(request).not.toHaveProperty('revision'); expect(request).not.toHaveProperty('status'); expect(request).not.toHaveProperty('default_mapped_model')
 })
 it('keeps creation null and update clear sentinels distinct without mutating the draft', () => {
  const draft = createGroupEditorDraft(); draft.name = 'Clear'; draft.fallback_group_id = null; draft.fallback_group_id_on_invalid_request = null
  expect(buildCreateGroupRequest(draft, details, false).image_price_1k).toBeNull()
  const updated = buildUpdateGroupRequest(draft, details, false)
  expect(updated).toMatchObject({ fallback_group_id: 0, fallback_group_id_on_invalid_request: 0, image_price_1k: -1, video_price_480p: -1, web_search_price_per_call: -1 })
  expect(draft.image_price_1k).toBeNull(); expect(draft.fallback_group_id).toBeNull()
 })
 it('projects simple-mode allowlists instead of sending hidden policy and commercial fields', () => {
  const draft = createGroupEditorDraft(); draft.name = 'Simple'; draft.rate_multiplier = 9; draft.rpm_limit = 800
  expect(buildCreateGroupRequest(draft, details, true)).toEqual({ name: 'Simple', description: '', i18n: {}, platform: 'anthropic' })
  expect(buildUpdateGroupRequest(draft, details, true)).toEqual({ name: 'Simple', description: '', i18n: {} })
 })
 it('keeps manifest creation disabled and preserves the selected edit accounts', () => {
  const draft = createGroupEditorDraft(); draft.name = 'Manifest'; draft.platform = 'openai'
  const selected = { ...details, manifest: { enabled: true, account_ids: [17, 22], fallback_to_scheduler: false } }
  expect(buildCreateGroupRequest(draft, selected, false).codex_models_manifest_config).toEqual(details.manifest)
  expect(buildUpdateGroupRequest(draft, selected, false).codex_models_manifest_config).toEqual(selected.manifest)
 })
 it.each(['openai', 'composite'] as const)('composes %s per-image billing into the same create/update command', platform => {
  const draft = createGroupEditorDraft()
  Object.assign(draft, { name: 'Images', platform, allow_image_generation: true })
  expect(buildCreateGroupRequest(draft, details, false).image_count_billing).toBe(false)
  draft.image_count_billing = true
  expect(buildCreateGroupRequest(draft, details, false).image_count_billing).toBe(true)
  expect(buildUpdateGroupRequest(draft, details, false).image_count_billing).toBe(true)
  draft.image_count_billing = false
  expect(buildUpdateGroupRequest(draft, details, false)).toHaveProperty('image_count_billing', false)
 })
 it.each(['anthropic', 'gemini', 'antigravity', 'grok'] as const)('clears per-image billing for the unsupported %s form', platform => {
  const draft = createGroupEditorDraft()
  Object.assign(draft, { platform, allow_image_generation: true, image_count_billing: true })
  expect(buildCreateGroupRequest(draft, details, false).image_count_billing).toBe(false)
  expect(buildUpdateGroupRequest(draft, details, false).image_count_billing).toBe(false)
  expect(draft.image_count_billing).toBe(true)
 })
 it('clears disabled image billing and omits the field in simple mode', () => {
  const draft = createGroupEditorDraft()
  Object.assign(draft, { platform: 'openai', image_count_billing: true, allow_image_generation: false })
  expect(buildUpdateGroupRequest(draft, details, false).image_count_billing).toBe(false)
  expect(buildCreateGroupRequest(draft, details, true)).not.toHaveProperty('image_count_billing')
  expect(buildUpdateGroupRequest(draft, details, true)).not.toHaveProperty('image_count_billing')
 })
})
