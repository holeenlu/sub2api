import { describe, it, expect } from 'vitest'
import type { Account } from '@/types'
import { accountParameterPatch } from '../accountParameterPatch'

describe('bulk account parameters', () => {
  it('only applies changed settings and excludes authorization, attribution and observations', () => {
    const account = { id: 1, created_by: 2, name: 'A', concurrency: 10, priority: 1, credentials: { access_token: 'private', model_mapping: { a: 'a' } }, extra: { vendor_identity_hash: 'owner', quota_used: 9, enable_session_id_masking: false } } as unknown as Account
    const patch = accountParameterPatch(account, { name: 'A', concurrency: 10, priority: 3, created_by: 999, credentials: { access_token: 'replacement', model_mapping: { a: 'b' } }, extra: { vendor_identity_hash: 'other', quota_used: 0, enable_session_id_masking: true } })
    expect(patch).toEqual({ priority: 3, credentials: { model_mapping: { a: 'b' } }, extra: { enable_session_id_masking: true } })
  })
  it('represents explicit setting removal without copying unrelated reference fields', () => {
    const account = { credentials: { custom_error_codes: [429] }, extra: { custom_base_url: 'https://example.invalid', unrelated: 1 }, notes: null, proxy_id: null } as unknown as Account
    expect(accountParameterPatch(account, { credentials: {}, extra: { unrelated: 1 }, notes: '', proxy_id: 0 })).toEqual({ credentials: { custom_error_codes: null }, extra: { custom_base_url: null } })
  })
  it('compares normalized form baselines without applying unrelated implicit defaults', () => {
    const account = { extra: {}, credentials: {} } as Account
    const baseline = { extra: { openai_long_context_billing_enabled: false, custom_option: true } }
    expect(accountParameterPatch(account, { extra: { openai_long_context_billing_enabled: false, custom_option: false } }, baseline))
      .toEqual({ extra: { custom_option: false } })
  })

})
