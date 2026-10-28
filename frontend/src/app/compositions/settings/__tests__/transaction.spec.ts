import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useSettingsTransaction, type SettingsTransport } from '../transaction'
import type { SystemSettings } from '@/features/system/api'
import { prepareSettingsPatch } from '../preparePatch'
const revisionA = 'a'.repeat(64), revisionB = 'b'.repeat(64)
const snapshot = (patch: Partial<SystemSettings> = {}): SystemSettings => ({ revision: revisionA, site_name: 'Original', payment_enabled: true, default_balance: 5, ...patch } as SystemSettings)
const scopes: ReturnType<typeof effectScope>[] = []
afterEach(() => { scopes.splice(0).forEach(scope => scope.stop()); vi.restoreAllMocks() })
async function setup(first = snapshot(), isCancelled?: (error: unknown) => boolean) {
  const scope = effectScope(); scopes.push(scope)
  const actor = ref<string | null>('admin:1:session:1')
  const get = vi.fn().mockResolvedValue(first)
  const put = vi.fn().mockResolvedValue(snapshot({ revision: revisionB }))
  const transport: SettingsTransport = { get, put, isCancelled }
  const state = scope.run(() => useSettingsTransaction(transport, actor))
  if (!state) throw new Error('Scope did not run')
  await flushPromises()
  return { state, actor, get, put }
}
describe('CT11 settings transaction', () => {
  it('runs real domain preparation once for auth grants, subscriptions, payment and Fast', async () => {
    const { state, put } = await setup()
    state.patch({ auth_source_default_email_subscriptions: [{ group_id: 3.9, validity_days: 60.8 }], auth_source_default_email_grant_on_signup: false, auth_source_default_email_balance: 0, default_subscriptions: [], payment_enabled: false, openai_fast_policy_settings: { rules: [] }, openai_oauth_scheduling_rate_multiplier: null, smtp_password: 'draft-only' })
    expect(await state.save(prepareSettingsPatch)).toBe(true)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith({ revision: revisionA, auth_source_default_email_subscriptions: [{ group_id: 3, validity_days: 60 }], auth_source_default_email_grant_on_signup: false, auth_source_default_email_balance: 0, default_subscriptions: [], payment_enabled: false, openai_fast_policy_settings: { rules: [] }, openai_oauth_scheduling_rate_multiplier: null, smtp_password: 'draft-only' }, { signal: expect.any(AbortSignal) })
    expect(state.draft.value?.smtp_password).toBe('')
  })
  it('keeps rejected subscription drafts and performs no partial request', async () => {
    const { state, put } = await setup()
    const subscriptions = [{ group_id: 4.2, validity_days: 30 }, { group_id: 4.8, validity_days: 90 }]
    state.patch({ auth_source_default_email_subscriptions: subscriptions, payment_enabled: false })
    expect(await state.save(prepareSettingsPatch)).toBe(false)
    expect(put).not.toHaveBeenCalled()
    expect(state.error.value).toEqual({ field: 'auth_source_default_email_subscriptions', messageKey: 'admin.settings.defaults.defaultSubscriptionsDuplicate', params: { groupId: 4 } })
    expect(state.draft.value?.auth_source_default_email_subscriptions).toEqual(subscriptions)
    expect(state.dirty.value).toEqual({ auth_source_default_email_subscriptions: subscriptions, payment_enabled: false })
    expect(state.revision.value).toBe(revisionA)
  })
  it('retains unnormalized input and the original revision after a prepared CAS conflict', async () => {
    const { state, put } = await setup()
    const subscriptions = [{ group_id: 4.2, validity_days: 30.8 }]
    state.patch({ auth_source_default_email_subscriptions: subscriptions })
    put.mockRejectedValueOnce({ status: 409, reason: 'SETTINGS_REVISION_CONFLICT' })
    expect(await state.save(prepareSettingsPatch)).toBe(false)
    expect(state.draft.value?.auth_source_default_email_subscriptions).toEqual(subscriptions)
    expect(state.revision.value).toBe(revisionA)
    expect(await state.save(prepareSettingsPatch)).toBe(false)
    expect(put).toHaveBeenCalledTimes(1)
  })
  it('sends one CAS PUT with only dirty fields, preserving false, zero and empty values', async () => {
    const { state, put } = await setup()
    state.patch({ site_name: '', payment_enabled: false, default_balance: 0, custom_menu_items: [] })
    expect(await state.save()).toBe(true)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith({ revision: revisionA, site_name: '', payment_enabled: false, default_balance: 0, custom_menu_items: [] }, { signal: expect.any(AbortSignal) })
    expect(state.dirtyFields.value).toEqual([])
    expect(state.revision.value).toBe(revisionB)
  })
  it('keeps omitted and read-only fields out of the request', async () => {
    const { state, put } = await setup()
    state.patch({ site_name: 'Updated', revision: 'forged', smtp_password_configured: true, openai_fast_policy_settings: undefined })
    await state.save()
    expect(put).toHaveBeenCalledWith({ revision: revisionA, site_name: 'Updated' }, { signal: expect.any(AbortSignal) })
  })
  it('does not save unchanged data or a reverted field', async () => {
    const { state, put } = await setup()
    expect(await state.save()).toBe(false)
    state.patch({ site_name: 'Editing' }); state.patch({ site_name: 'Original' })
    expect(await state.save()).toBe(false)
    expect(put).not.toHaveBeenCalled()
  })
  it('submits auth defaults, payment and Fast atomically', async () => {
    const { state, put } = await setup()
    state.patch({ auth_source_default_email_balance: 9, payment_min_amount: 2, openai_fast_policy_settings: { rules: [] } })
    await state.save()
    expect(put).toHaveBeenCalledTimes(1)
    expect(put.mock.calls[0][0]).toEqual({ revision: revisionA, auth_source_default_email_balance: 9, payment_min_amount: 2, openai_fast_policy_settings: { rules: [] } })
  })
  it('retains baseline, draft and revision on 409; comparison never replays writes', async () => {
    const { state, put, get } = await setup()
    state.patch({ site_name: 'Mine', payment_enabled: false })
    get.mockResolvedValue(snapshot({ revision: revisionB, site_name: 'Theirs', default_balance: 8 }))
    put.mockRejectedValueOnce({ status: 409, reason: 'SETTINGS_REVISION_CONFLICT' })
    expect(await state.save()).toBe(false)
    expect(state.baseline.value?.site_name).toBe('Original')
    expect(state.draft.value?.site_name).toBe('Mine')
    expect(state.revision.value).toBe(revisionA)
    expect(state.conflict.value?.server.site_name).toBe('Theirs')
    expect(await state.save()).toBe(false)
    expect(put).toHaveBeenCalledTimes(1)
    state.mergeReviewed(['payment_enabled'])
    expect(state.revision.value).toBe(revisionB)
    expect(state.draft.value?.site_name).toBe('Theirs')
    expect(state.draft.value?.default_balance).toBe(8)
    expect(state.dirty.value).toEqual({ payment_enabled: false })
    expect(put).toHaveBeenCalledTimes(1)
    await state.save()
    expect(put).toHaveBeenLastCalledWith({ revision: revisionB, payment_enabled: false }, { signal: expect.any(AbortSignal) })
  })
  it('handles 428 without requiring reason and retains edits', async () => {
    const { state, put } = await setup()
    state.patch({ site_name: 'Mine' }); put.mockRejectedValueOnce({ status: 428, message: 'Revision required' })
    await state.save()
    expect(state.revisionMissing.value).toBe(true)
    expect(state.draft.value?.site_name).toBe('Mine')
  })
  it('rejects missing read revisions without inventing one', async () => {
    const { state, put } = await setup(snapshot({ revision: undefined }))
    expect(state.error.value).toBeInstanceOf(Error)
    expect(state.draft.value).toBeNull()
    expect(await state.save()).toBe(false)
    expect(put).not.toHaveBeenCalled()
  })
  it('blocks duplicate saves and retains failed drafts', async () => {
    const { state, put } = await setup()
    state.patch({ site_name: 'Mine' })
    let reject: (cause: unknown) => void = () => {}
    put.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    const pending = state.save()
    expect(await state.save()).toBe(false)
    reject({ status: 400, message: 'Invalid' }); await pending
    expect(state.draft.value?.site_name).toBe('Mine')
    expect(put).toHaveBeenCalledTimes(1)
  })
  it('ignores a saved response after logout', async () => {
    const { state, put, actor } = await setup()
    state.patch({ smtp_password: 'transient-secret' })
    let resolve: (value: SystemSettings) => void = () => {}
    put.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const pending = state.save(); actor.value = null; await nextTick()
    resolve(snapshot({ revision: revisionB })); await pending
    expect(state.baseline.value).toBeNull()
    expect(state.draft.value).toBeNull()
    expect(state.dirtyFields.value).toEqual([])
  })
  it('clears secret drafts on success without persisting them', async () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem')
    const { state, put } = await setup()
    state.patch({ smtp_password: 'transient-secret' }); await state.save()
    expect(put).toHaveBeenCalledWith({ revision: revisionA, smtp_password: 'transient-secret' }, { signal: expect.any(AbortSignal) })
    expect(state.draft.value?.smtp_password).toBe('')
    expect(spy).not.toHaveBeenCalled()
  })
  it('preserves an explicit nullable scheduling rate', async () => {
    const { state, put } = await setup(snapshot({ openai_oauth_scheduling_rate_multiplier: 2 }))
    state.patch({ openai_oauth_scheduling_rate_multiplier: null }); await state.save()
    expect(put).toHaveBeenCalledWith({ revision: revisionA, openai_oauth_scheduling_rate_multiplier: null }, { signal: expect.any(AbortSignal) })
  })
  it('blocks 428 replays until the user reviews a fresh revision', async () => {
    const { state, put } = await setup()
    state.patch({ site_name: 'Mine' }); put.mockRejectedValueOnce({ status: 428 })
    await state.save(); await state.save()
    expect(put).toHaveBeenCalledTimes(1)
  })
  it('rejects late comparison responses and includes edits made during review', async () => {
    const { state, get } = await setup()
    state.patch({ site_name: 'Mine' })
    let first: (value: SystemSettings) => void = () => {}
    let second: (value: SystemSettings) => void = () => {}
    get.mockReturnValueOnce(new Promise(done => { first = done })).mockReturnValueOnce(new Promise(done => { second = done }))
    const older = state.compare(), latest = state.compare()
    second(snapshot({ revision: revisionB, site_name: 'Latest' })); await latest
    first(snapshot({ revision: 'c'.repeat(64), site_name: 'Stale' })); await older
    expect(state.conflict.value?.server.site_name).toBe('Latest')
    state.patch({ payment_enabled: false })
    expect(state.conflict.value?.fields).toContain('payment_enabled')
    expect(state.revision.value).toBe(revisionA)
  })

  it('does not replay an uncertain network result and requires comparison', async () => {
    const { state, put, get } = await setup()
    state.patch({ site_name: 'Mine' }); put.mockRejectedValueOnce({ status: 0, code: 'ERR_NETWORK', message: 'Network unavailable' })
    await state.save(); await state.save()
    expect(state.uncertain.value).toBe(true)
    expect(state.draft.value?.site_name).toBe('Mine')
    expect(put).toHaveBeenCalledTimes(1)
    get.mockResolvedValue(snapshot({ revision: revisionB, site_name: 'Mine' }))
    await state.compare(); state.mergeReviewed([])
    expect(state.uncertain.value).toBe(false)
    expect(state.dirtyFields.value).toEqual([])
  })
  it('treats blank configured secret inputs as unchanged under the existing preserve-empty contract', async () => {
    const { state, put } = await setup(snapshot({ smtp_password_configured: true }))
    state.patch({ smtp_password: '', site_name: 'Updated' }); await state.save()
    expect(put).toHaveBeenCalledWith({ revision: revisionA, site_name: 'Updated' }, { signal: expect.any(AbortSignal) })
  })

  it('keeps a cancelled step-up draft without an error or an uncertain-save warning', async () => {
    const cancelled = { code: 'STEP_UP_CANCELLED' }
    const { state, put } = await setup(snapshot(), error => error === cancelled)
    state.patch({ step_up_enabled: false }); put.mockRejectedValueOnce(cancelled)
    expect(await state.save()).toBe(false)
    expect(state.error.value).toBeNull()
    expect(state.uncertain.value).toBe(false)
    expect(state.dirty.value).toEqual({ step_up_enabled: false })
  })

})
