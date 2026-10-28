import { describe, expect, it } from 'vitest'
import { emptyKeyDraft, editKeyDraft, createKeyPayload, updateKeyPayload, validateKeyDraft } from '../model/keyDraft'
import { parseKeyImport } from '../model/keyTransfer'
import { keyFixture } from './keyFixture'
describe('Key drafts preserve server command semantics', () => {
  it('isolates edits from the loaded server row and explicitly clears disabled restrictions', () => {
    const key = keyFixture(); const draft = editKeyDraft(key)
    draft.name = 'Changed'; draft.enable_ip_restriction = false; draft.enable_rate_limit = false; draft.quota = 0
    expect(key.name).toBe('Client'); expect(key.ip_whitelist).toEqual(['10.0.0.0/8'])
    expect(updateKeyPayload(draft, key)).toMatchObject({ name: 'Changed', ip_whitelist: [], ip_blacklist: [],
      quota: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0, expires_at: '' })
  })
  it.each(['quota_exhausted', 'expired'] as const)('does not silently turn %s into inactive when saving unrelated fields', status => {
    const key = { ...keyFixture(), status }; const draft = editKeyDraft(key)
    expect(updateKeyPayload(draft, key)).not.toHaveProperty('status')
    draft.status = 'active'; expect(updateKeyPayload(draft, key).status).toBe('active')
  })
  it('retains custom-key validation, whitespace-trimmed IP entries and create expiry in whole days', () => {
    const draft = emptyKeyDraft()
    expect(validateKeyDraft(draft, false)).toBe('keys.groupRequired')
    draft.group_id = 7; draft.use_custom_key = true
    expect(validateKeyDraft(draft, false)).toBe('keys.customKeyRequired')
    draft.custom_key = 'short'; expect(validateKeyDraft(draft, false)).toBe('keys.customKeyTooShort')
    draft.custom_key = 'bad key too long!'; expect(validateKeyDraft(draft, false)).toBe('keys.customKeyInvalidChars')
    draft.custom_key = 'sk-valid_custom-1234'; draft.enable_ip_restriction = true; draft.ip_whitelist = ' 10.0.0.0/8\n\n127.0.0.1 '
    draft.enable_expiration = true; draft.expiration_date = '2026-10-27T00:00:00Z'
    expect(validateKeyDraft(draft, false)).toBe('')
    expect(createKeyPayload(draft, Date.parse('2026-10-25T12:00:00Z'))).toMatchObject({ custom_key: draft.custom_key, expires_in_days: 2, ip_whitelist: ['10.0.0.0/8', '127.0.0.1'] })
  })
  it('rejects nonfinite amounts and dates before submitting', () => {
    const draft = { ...emptyKeyDraft(), group_id: 7, quota: Infinity }
    expect(validateKeyDraft(draft, false)).toBe('keys.bulkEdit.invalidLimit')
    draft.quota = 0; draft.enable_expiration = true; draft.expiration_date = 'not a date'
    expect(validateKeyDraft(draft, true)).toBe('keys.bulkEdit.invalidExpiration')
  })
  it('retains the v1 transfer envelope and leaves per-row failures to the server', () => {
    const backup = { type: 'sub2api-keys', version: 1, keys: [{ status: 'disabled', key: 'sk-original' }] }
    expect(parseKeyImport('\uFEFF' + JSON.stringify(backup))).toEqual(backup)
    expect(() => parseKeyImport(JSON.stringify({ ...backup, version: 2 }))).toThrow()
    expect(() => parseKeyImport(JSON.stringify({ ...backup, keys: [] }))).toThrow()
  })
})
