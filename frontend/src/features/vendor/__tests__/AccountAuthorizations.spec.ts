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
  const w = render(condition === 'readOnly', { ...account, archived: condition === 'archived', bps_models: ['explicit-image-model'],
    authorizations: account.authorizations?.map(grant => grant.mode === 'bps' && condition === 'serverRevoked'
      ? { ...grant, status: 'policy_revoked', can_refresh: false } : grant) },
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
  const w = render(false, { ...account, authorization_modes: ['native'], authorizations: [
    { mode: 'native', authorized: true, enabled: true, status: 'expired', can_refresh: true },
    { mode: 'bps', authorized: false, enabled: false, status: 'unconfigured', can_refresh: false }
  ] })
  expect(w.get('[data-testid=authorize-native]').text()).toBe(en.reauthorize)
  expect(w.get('[data-testid=authorize-bps]').text()).toBe(en.authorization.attach)
  expect(w.find('[data-testid=revoke-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=enable-bps]').exists()).toBe(false)
  expect(w.find('[data-testid=refresh-bps]').exists()).toBe(false)
  w.unmount()
})
it('stores an explicit model list without implicitly enabling images or the BPS mode', async () => {
  const w = render()
  const form = w.get('[data-testid=bps-model-settings] form')
  expect(w.get('[data-testid=bps-all-models]').element).toHaveProperty('checked', true)
  await w.get('[data-testid=bps-all-models]').setValue(false)
  await form.get('textarea').setValue('text-model\nimage-model\ntext-model')
  await form.trigger('submit')
  expect(w.emitted('update')).toEqual([[{ bps_all_models: false, bps_models: ['text-model', 'image-model'] }]])
  await w.setProps({ account: { ...account, bps_models: ['text-model', 'image-model'] } })
  await flushPromises()
  await w.get('[data-testid=bps-all-models]').setValue(true)
  await form.trigger('submit')
  expect(w.emitted('update')?.[1]).toEqual([{ bps_all_models: true }])
  w.unmount()
})
it('allows an explicit empty model list and explains that it stops BPS routing', async () => {
  const w = render()
  await w.get('[data-testid=bps-all-models]').setValue(false)
  expect(w.text()).toContain(en.authorization.noModelsHelp)
  await w.get('[data-testid=bps-model-settings] form').trigger('submit')
  expect(w.emitted('update')).toEqual([[{ bps_all_models: false, bps_models: [] }]])
  w.unmount()
})
