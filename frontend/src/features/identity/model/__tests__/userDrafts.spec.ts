import { describe, expect, it } from 'vitest'
import { newUserDraft, userEditDraft, createUserPayload, updateUserPayload } from '../userDrafts'
import type { AdminUser } from '../../api/profileTypes'

describe('user editor payloads', () => {
  it('preserves an explicit zero initial balance and omits a blank balance', () => {
    const draft = newUserDraft()
    expect(createUserPayload(draft)).not.toHaveProperty('balance')
    draft.balance = '0'
    expect(createUserPayload(draft).balance).toBe(0)
  })
  it('never carries an entered initial balance into a super-admin creation', () => {
    const draft = newUserDraft()
    draft.balance = '200'
    draft.role = 'super_admin'
    expect(createUserPayload(draft)).not.toHaveProperty('balance')
  })
  it('preserves unlimited zero concurrency and vendor role while excluding attribute data from the user command', () => {
    const draft = userEditDraft({ email: 'vendor@example.test', username: 'Vendor', role: 'vendor_level1', concurrency: 0, rpm_limit: 0 } as AdminUser)
    draft.customAttributes = { 9: 'private attribute' }
    expect(updateUserPayload(draft)).toMatchObject({ role: 'vendor_level1', concurrency: 0, rpm_limit: 0 })
    expect(updateUserPayload(draft)).not.toHaveProperty('customAttributes')
    expect(updateUserPayload(draft)).not.toHaveProperty('password')
    expect(updateUserPayload(draft)).not.toHaveProperty('reset_totp')
  })
  it('includes security changes only when explicitly supplied', () => {
    const draft = userEditDraft()
    draft.password = '  a-new-password  '
    draft.reset_totp = true
    expect(updateUserPayload(draft)).toMatchObject({ password: 'a-new-password', reset_totp: true })
  })
})
