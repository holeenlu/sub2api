import { describe, it, expect } from 'vitest'
import { accountParameterPatch } from '@/features/resources/model/accountParameterPatch'
import { accountFixture } from './boundaryFixtures'

describe('bulk account parameters', () => {
  it('only applies changed settings and excludes authorization, attribution and observations', () => {
    const account = accountFixture({ id: 1, created_by: 2, name: 'A', concurrency: 10, priority: 1, credentials: { access_token: 'private', model_mapping: { a: 'a' } }, extra: { vendor_identity_hash: 'owner', quota_used: 9, enable_session_id_masking: false } })
    const patch = accountParameterPatch(account, { name: 'A', concurrency: 10, priority: 3, created_by: 999, credentials: { access_token: 'replacement', model_mapping: { a: 'b' } }, extra: { vendor_identity_hash: 'other', quota_used: 0, enable_session_id_masking: true } })
    expect(patch).toEqual({ priority: 3, credentials: { model_mapping: { a: 'b' } }, extra: { enable_session_id_masking: true } })
  })
  it('represents explicit setting removal without copying unrelated reference fields', () => {
    const account = accountFixture({ credentials: { custom_error_codes: [429] }, extra: { custom_base_url: 'https://example.invalid', unrelated: 1 }, notes: null, proxy_id: null })
    expect(accountParameterPatch(account, { credentials: {}, extra: { unrelated: 1 }, notes: '', proxy_id: 0 })).toEqual({ credentials: { custom_error_codes: null }, extra: { custom_base_url: null } })
  })
  it('compares normalized form baselines without applying unrelated implicit defaults', () => {
    const account = accountFixture({ extra: {}, credentials: {} })
    const baseline = { extra: { openai_long_context_billing_enabled: false, custom_option: true } }
    expect(accountParameterPatch(account, { extra: { openai_long_context_billing_enabled: false, custom_option: false } }, baseline))
      .toEqual({ extra: { custom_option: false } })
  })
  it('projects every allowed top-level parameter without copying account identity or platform', () => {
    const parameters = {
      name: 'Edited name', notes: 'Edited notes', proxy_id: 5, concurrency: 3,
      priority: 0, rate_multiplier: 0, load_factor: 4, status: 'inactive',
      schedulable: false, group_ids: [9], expires_at: 42, auto_pause_on_expired: true,
      upstream_billing_probe_enabled: false, upstream_billing_rate_sync_enabled: false,
    }
    const patch = accountParameterPatch(accountFixture(), {
      ...parameters, id: 100, platform: 'openai', type: 'apikey',
      vendor_id: 200, created_by: 300, created_by_role: 'superadmin',
      authorization_modes: ['native'], credentials_status: { has_api_key: true },
      current_concurrency: 99, quota_used: 1000, allowAuthorize: true,
      allowRateEdit: true, allowTransportEdit: true, supplierScope: false,
      confirm_mixed_channel_risk: true, unexpected: 'ignored',
    })
    expect(patch).toStrictEqual(parameters)
  })

  it('distinguishes omitted properties from explicitly present undefined values', () => {
    const account = accountFixture({ notes: 'Existing notes', concurrency: 7 })
    expect(accountParameterPatch(account, {})).toStrictEqual({})
    const patch = accountParameterPatch(account, { notes: undefined, concurrency: undefined })
    expect(patch).toStrictEqual({ notes: undefined, concurrency: undefined })
    expect(Object.keys(patch)).toEqual(['notes', 'concurrency'])
    expect(JSON.stringify(patch)).toBe('{}')
  })

  it.each([
    { label: 'false', payload: { schedulable: false }, expected: { schedulable: false } },
    { label: 'zero', payload: { concurrency: 0, priority: 0 }, expected: { concurrency: 0, priority: 0 } },
    { label: 'null', payload: { notes: null, proxy_id: null, expires_at: null }, expected: { notes: null, proxy_id: null, expires_at: null } },
    { label: 'empty groups', payload: { group_ids: [] }, expected: { group_ids: [] } },
  ])('preserves explicit $label values', ({ payload, expected }) => {
    const account = accountFixture({ notes: 'Existing notes', proxy_id: 8, expires_at: 42, group_ids: [1] })
    expect(accountParameterPatch(account, payload)).toStrictEqual(expected)
  })

  it('keeps the existing empty-form normalization for missing or null account fields', () => {
    for (const notes of [undefined, null]) {
      const account = accountFixture({ notes, proxy_id: null, load_factor: null, expires_at: null })
      expect(accountParameterPatch(account, { notes: '', proxy_id: 0, load_factor: 0, expires_at: 0 }))
        .toStrictEqual({})
    }
  })

  it('uses the form baseline even when its values differ from the account', () => {
    const account = accountFixture({ concurrency: 7, schedulable: true, group_ids: [1] })
    const baseline = { concurrency: 0, schedulable: false, group_ids: [], notes: undefined }
    expect(accountParameterPatch(account, { concurrency: 0, schedulable: false, group_ids: [], notes: '' }, baseline))
      .toStrictEqual({})
    expect(accountParameterPatch(account, {}, baseline)).toStrictEqual({})
  })

  it('does not fill missing baseline properties from the reference account', () => {
    const account = accountFixture({ name: 'Reference', concurrency: 7 })
    expect(accountParameterPatch(account, { name: 'Reference', concurrency: 7 }, {}))
      .toStrictEqual({ name: 'Reference', concurrency: 7 })
  })

  it('normalizes string expiry baselines to seconds without discarding an explicit clear', () => {
    const account = accountFixture({ expires_at: 1 })
    const baseline = { expires_at: '2026-10-27T00:00:00.999Z' }
    const expiry = Math.floor(Date.parse(baseline.expires_at) / 1000)
    expect(accountParameterPatch(account, { expires_at: expiry }, baseline)).toStrictEqual({})
    expect(accountParameterPatch(account, { expires_at: null }, baseline)).toStrictEqual({ expires_at: null })
    expect(accountParameterPatch(account, { expires_at: 0 }, baseline)).toStrictEqual({ expires_at: 0 })
  })

  it.each(['upstream_billing_probe_enabled', 'upstream_billing_rate_sync_enabled'] as const)(
    'compares request-only %s with the form baseline without inventing an extra fallback',
    key => {
      const account = accountFixture({ extra: { [key]: true } })
      expect(accountParameterPatch(account, {}, { [key]: true })).toStrictEqual({})
      expect(accountParameterPatch(account, { [key]: true }, { [key]: true })).toStrictEqual({})
      expect(accountParameterPatch(account, { [key]: false }, { [key]: true })).toStrictEqual({ [key]: false })
      expect(accountParameterPatch(account, { [key]: true })).toStrictEqual({ [key]: true })
      expect(accountParameterPatch(account, { [key]: false })).toStrictEqual({ [key]: false })
      expect(accountParameterPatch(account, { [key]: undefined })).toStrictEqual({})
    },
  )

  it.each(['credentials', 'extra'] as const)('preserves %s setting clears, false and zero', key => {
    const account = accountFixture({ [key]: { removed: true, changed: true } })
    const payload = { [key]: { changed: undefined, disabled: false, limit: 0 } }
    const baseline = { [key]: { removed: true, changed: true } }
    expect(accountParameterPatch(account, payload, baseline)).toStrictEqual({
      [key]: { removed: null, changed: null, disabled: false, limit: 0 },
    })
    expect(account[key]).toStrictEqual({ removed: true, changed: true })
    expect(payload[key]).toStrictEqual({ changed: undefined, disabled: false, limit: 0 })
    expect(baseline[key]).toStrictEqual({ removed: true, changed: true })
  })

  it.each(['credentials', 'extra'] as const)('retains %s empty defaults only when a form baseline exists', key => {
    const account = accountFixture({ [key]: {} })
    const payload = { [key]: { empty: '', missing: null, object: {}, disabled: false, limit: 0 } }
    expect(accountParameterPatch(account, payload)).toStrictEqual({ [key]: { disabled: false, limit: 0 } })
    expect(accountParameterPatch(account, payload, { [key]: {} })).toStrictEqual(payload)
  })

  it.each(['credentials', 'extra'] as const)('does not turn protected %s fields into writes or clears', key => {
    const protectedFields = {
      access_token: 'synthetic-token', API_KEY: 'synthetic-key', account_uuid: 'synthetic-id',
      vendor_identity_hash: 'owner', passive_usage_tokens: 5, client_telemetry_enabled: true,
      upstream_billing_probe: { status: 'ok' }, quota_daily_used: 9,
    }
    const account = accountFixture({ [key]: protectedFields })
    const baseline = { [key]: protectedFields }
    expect(accountParameterPatch(account, { [key]: {} }, baseline)).toStrictEqual({})
    expect(accountParameterPatch(accountFixture(), { [key]: protectedFields }, { [key]: {} }))
      .toStrictEqual({})
  })
})
