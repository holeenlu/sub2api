import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { expect, it } from 'vitest'
import en from '@/i18n/locales/en/vendor'
import zh from '@/i18n/locales/zh/vendor'
import ja from '@/i18n/locales/ja/vendor'
import zhTW from '@/i18n/locales/zh-TW/vendor'
import AccountAuthorizations from '../components/AccountAuthorizations.vue'
import type { Vendor, VendorAccount } from '../types'

const vendor = { platforms: ['openai'], openai_authorization_modes: ['native', 'bps'] } as Vendor
const account = {
  id: 12, platform: 'openai', type: 'oauth', authorization_modes: ['native', 'bps'], bps_models: null,
  authorizations: [
    { mode: 'native', authorized: true, enabled: true, status: 'active', can_refresh: true },
    { mode: 'bps', authorized: true, enabled: false, status: 'disabled', can_refresh: true }
  ]
} as VendorAccount
function render(readOnly = false, value = account, policy = vendor, locale = 'en', messages = en) {
  return mount(AccountAuthorizations, {
    props: { account: value, vendor: policy, readOnly, busy: false },
    global: { plugins: [createI18n({ legacy: false, locale, messages: { [locale]: { vendor: messages } } })] }
  })
}
it.each([['en', en], ['zh', zh], ['ja', ja], ['zh-TW', zhTW]] as const)('shows independent grant and dispatch states in %s', async (locale, messages) => {
  const w = render(false, account, vendor, locale, messages)
  expect(w.get('[data-authorization=native] [data-authorization-status]').text()).toBe(messages.authorization.status.active)
  expect(w.get('[data-authorization=bps] [data-authorization-status]').text()).toBe(messages.authorization.status.disabled)
  expect(w.get('[data-testid=enable-native]').attributes('aria-checked')).toBe('true')
  expect(w.get('[data-testid=enable-bps]').attributes('aria-checked')).toBe('false')
  await w.get('[data-testid=enable-bps]').trigger('click')
  expect(w.emitted('update')).toEqual([[{ bps_enabled: true }]])
  await w.get('[data-testid=revoke-native]').trigger('click')
  expect(w.emitted('revoke')).toEqual([['native']])
  await w.get('[data-testid=refresh-bps]').trigger('click')
  expect(w.emitted('refresh')).toEqual([['bps']])
  w.unmount()
})
it.each(['readOnly', 'archived', 'policyRevoked', 'serverRevoked'] as const)('does not offer BPS writes for %s resources', condition => {
  const w = render(condition === 'readOnly', {
    ...account, archived: condition === 'archived', bps_models: ['explicit-image-model'],
    authorizations: account.authorizations?.map(grant => grant.mode === 'bps' && condition === 'serverRevoked'
      ? { ...grant, status: 'policy_revoked', can_refresh: false } : grant)
  },
    condition === 'policyRevoked' ? { ...vendor, openai_authorization_modes: ['native'] } : vendor)
  expect(w.find('[data-testid=authorize-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=enable-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=revoke-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=refresh-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=bps-model-settings] form').exists()).toBe(false)
  expect(w.text()).toContain('explicit-image-model')
  w.unmount()
})
it('offers attach for an unconfigured mode, and reauthorization for an expired grant', () => {
  const w = render(false, {
    ...account, authorization_modes: ['native'], authorizations: [
      { mode: 'native', authorized: true, enabled: true, status: 'expired', can_refresh: true },
      { mode: 'bps', authorized: false, enabled: false, status: 'unconfigured', can_refresh: false }
    ]
  })
  expect(w.get('[data-testid=authorize-native]').text()).toBe(en.reauthorize)
  expect(w.get('[data-testid=authorize-bps]').text()).toBe(en.authorization.attach)
  expect(w.find('[data-testid=revoke-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=enable-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=refresh-bps]').exists()).toBe(false)
  w.unmount()
})
it('stores only explicit model IDs without enabling either grant', async () => {
  const w = render()
  try {
    const form = w.get('[data-testid=bps-model-settings] form')
    expect(w.find('[data-testid=bps-all-models]').exists()).toBe(false)
    await form.get('textarea').setValue('text-model\nimage-model\ntext-model')
    await form.trigger('submit')
    expect(w.emitted('update')).toEqual([[{ bps_models: ['text-model', 'image-model'] }]])
  } finally { w.unmount() }
})

it('copies the exact safe disabled timestamp only for an explicit BPS enable', async () => {
  const disabledAt = '2026-10-27T01:02:03.000004+00:00'
  const w = render(false, { ...account, excel_bps_403_recovery: { state: 'paused', interval_minutes: 60, disabled_at: disabledAt } })
  try {
    await w.get('[data-testid=enable-bps]').trigger('click')
    expect(w.emitted('update')).toEqual([[{ bps_enabled: true, bps_expected_disabled_at: disabledAt }]])
  } finally { w.unmount() }
})

it('lets the owner explicitly keep BPS off and stop recovery without a condition', async () => {
  const w = render(false, { ...account, excel_bps_403_recovery: { state: 'pending', interval_minutes: 60, disabled_at: '2026-10-27T01:00:00Z' } })
  try {
    await w.get('[data-testid=stop-bps-recovery]').trigger('click')
    expect(w.emitted('update')).toEqual([[{ bps_enabled: false }]])
  } finally { w.unmount() }
})
it('allows an explicit empty list to stop selecting BPS models', async () => {
  const w = render(false, { ...account, bps_models: ['text-model'] })
  try {
    await w.get('textarea').setValue('')
    expect(w.text()).toContain(en.authorization.noModelsHelp)
    await w.get('[data-testid=bps-model-settings] form').trigger('submit')
    expect(w.emitted('update')).toEqual([[{ bps_models: [] }]])
  } finally { w.unmount() }
})
it.each([undefined, null, 'text-model', {}, [], [42, null, false, '', '  ']])('treats malformed/missing legacy BPS scope as no selection: %j', async raw => {
  const w = render(false, Object.assign({}, account, { bps_models: raw, bps_all_models: true }))
  try {
    expect(w.find('[data-testid=bps-all-models]').exists()).toBe(false)
    expect(w.get('textarea').element).toHaveProperty('value', '')
    expect(w.text()).toContain(en.authorization.noModelsHelp)
    await w.get('textarea').setValue('chosen-model')
    await w.get('[data-testid=bps-model-settings] form').trigger('submit')
    expect(w.emitted('update')).toEqual([[{ bps_models: ['chosen-model'] }]])
  } finally { w.unmount() }
})
it('resets an unsaved list when switching between accounts with the same missing list', async () => {
  const w = render()
  try {
    await w.get('textarea').setValue('previous-account-model')
    await w.setProps({ account: { ...account, id: 99 } })
    await flushPromises()
    expect(w.get('textarea').element).toHaveProperty('value', '')
    await w.get('[data-testid=bps-model-settings] form').trigger('submit')
    expect(w.emitted('update')).toBeUndefined()
  } finally { w.unmount() }
})
it('does not save a drafted selection after permission is revoked or while busy', async () => {
  const w = render()
  try {
    await w.get('textarea').setValue('chosen-model')
    await w.setProps({ busy: true })
    await w.get('form').trigger('submit')
    expect(w.emitted('update')).toBeUndefined()
    await w.setProps({ busy: false, vendor: { ...vendor, openai_authorization_modes: ['native'] } })
    expect(w.find('[data-testid=bps-model-settings] form').exists()).toBe(false)
    expect(w.emitted('update')).toBeUndefined()
  } finally { w.unmount() }
})
it('filters non-string entries consistently with backend explicit matching', () => {
  const w = render(true, Object.assign({}, account, { bps_models: [42, ' a ', false, 'a', 'b'] }))
  try { expect(w.get('[data-testid=bps-model-settings] p:last-child').text()).toBe('a, b') }
  finally { w.unmount() }
})
