import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AccountWorkbench from '../components/AccountWorkbench.vue'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import { supplyServicesKey } from '@/features/supply/services'
import { createSupplyTestServices } from '@/features/supply/__tests__/services'
import type { ResourceScope, Vendor, VendorAccount } from '@/features/supply/types'
import messages from '@/features/supply/locales/en'
import resourceMessages from '@/features/resources/locales/en'

const api = vi.hoisted(() => ({ accounts: vi.fn(), account: vi.fn(), updateAccount: vi.fn(), proxies: vi.fn(), scope: vi.fn() }))
vi.mock('@/features/supply/api', () => ({
  resourceAPI: (scope: ResourceScope) => { api.scope(scope); return api }, vendorAPI: api
}))
const account: VendorAccount = {
  id: 12, name: 'BPS selection', vendor_id: 8, platform: 'openai', type: 'oauth',
  authorization_modes: ['native', 'bps'], bps_models: null,
  authorizations: [
    { mode: 'native', authorized: true, enabled: true, status: 'active', can_refresh: true },
    { mode: 'bps', authorized: true, enabled: true, status: 'active', can_refresh: true }
  ],
  proxy_id: null, group_ids: [], groups: [], concurrency: 3, priority: 50, status: 'active',
  archived: false, archived_at: null, schedulable: true, created_at: '2026-10-27T00:00:00Z'
}
const vendor: Vendor = {
  id: 8, user_id: 1, name: 'Supplier', status: 'active', policy_version: 1, contract_version: 1,
  platforms: ['openai'], openai_authorization_modes: ['native', 'bps'],
  account_defaults: { concurrency: 3, priority: 50 }, group_ids: [], groups: [],
  account_limit: 100, proxy_limit: 10, allow_direct: true, contact_name: '', settlement_email: '',
  binance_id: '', default_coefficient: '1', group_coefficients: {}
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  api.accounts.mockResolvedValue({ items: [account], total: 1, page: 1, page_size: 20 })
  api.account.mockResolvedValue(account)
  api.updateAccount.mockResolvedValue(account)
  api.proxies.mockResolvedValue({ items: [], total: 0 })
})
async function open(scope: ResourceScope) {
  const wrapper = mount(AccountWorkbench, {
    props: { scope, vendor },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { ...resourceMessages, ...messages } } })],
      provide: { [supplyServicesKey as symbol]: createSupplyTestServices(), [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } },
      stubs: {
        teleport: true, Pagination: true, GroupBadge: true, TotpStepUpDialog: true,
        AccountForm: true, AccountImport: true, UsageWorkbench: true, AccountStatusIndicator: true,
        BaseDialog: { template: '<section role="dialog"><slot /><slot name="footer" /></section>' }
      }
    }
  })
  await flushPromises()
  const details = wrapper.findAll('button').find(button => button.text() === messages.vendor.details)
  if (!details) throw new Error('Account details action missing')
  await details.trigger('click')
  await flushPromises()
  return wrapper
}
it.each([{ admin: false }, { admin: true, vendorId: 8 }])('saves explicit BPS models with one scoped request: %j', async scope => {
  const wrapper = await open(scope)
  expect(wrapper.find('[data-testid=bps-all-models]').exists()).toBe(false)
  await wrapper.get('[data-testid=bps-model-settings] textarea').setValue('selected-model')
  await wrapper.get('[data-testid=bps-model-settings] form').trigger('submit')
  await flushPromises()
  expect(api.updateAccount).toHaveBeenCalledTimes(1)
  expect(api.updateAccount).toHaveBeenCalledWith(12, { bps_models: ['selected-model'] }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(api.scope).toHaveBeenLastCalledWith(scope)
})
it('does not offer model edits in a read-only scope', async () => {
  const wrapper = await open({ admin: false, readOnly: true })
  expect(wrapper.find('[data-testid=bps-model-settings] form').exists()).toBe(false)
  expect(api.updateAccount).not.toHaveBeenCalled()
})


function disabledAccount(disabledAt = '2026-10-27T01:02:03.000004Z'): VendorAccount {
  return { ...account, bps_models: ['mapped-model'],
    excel_bps_403_recovery: { state: 'pending', interval_minutes: 60, disabled_at: disabledAt },
    authorizations: account.authorizations?.map(value => value.mode === 'bps' ? { ...value, enabled: false } : value) }
}

it('displays the safe recovery state in supplier rows and details without private data', async () => {
  const disabled = disabledAccount()
  disabled.excel_bps_403_recovery = { state: 'reauthorization_required', reason: 'grant_missing', interval_minutes: 60 }
  disabled.extra = { openai_excel_bps_failure: { secret: 'PRIVATE_FAILURE_RECORD' } }
  api.accounts.mockResolvedValue({ items: [disabled], total: 1, page: 1, page_size: 20 })
  api.account.mockResolvedValue(disabled)
  const wrapper = await open({ admin: false })
  const badges = wrapper.findAll('[data-test="bps-failure-badge"]')
  expect(badges).toHaveLength(2)
  for (const badge of badges) expect(badge.text()).toBe(resourceMessages.admin.accounts.openai.excelBPSFailureReauth)
  expect(wrapper.html()).not.toContain('PRIVATE_FAILURE_RECORD')
})

it('never retries a supplier 409 and requires an explicit refresh before re-enabling', async () => {
  const initial = disabledAccount(), newer = disabledAccount('2026-10-27T02:03:04.000009+00:00')
  api.account.mockResolvedValue(initial)
  api.updateAccount.mockRejectedValueOnce({ status: 409, reason: 'OPENAI_EXCEL_BPS_STATE_CONFLICT' }).mockResolvedValue(newer)
  const wrapper = await open({ admin: false })
  await wrapper.get('[data-testid=enable-bps]').trigger('click'); await flushPromises()
  expect(api.updateAccount).toHaveBeenCalledTimes(1)
  expect(api.updateAccount).toHaveBeenCalledWith(12, { bps_enabled: true, bps_expected_disabled_at: initial.excel_bps_403_recovery?.disabled_at }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(wrapper.get('[data-testid=bps-state-conflict]').text()).toContain(messages.vendor.authorization.stateConflict)
  await wrapper.get('[data-testid=enable-bps]').trigger('click'); await flushPromises()
  expect(api.updateAccount).toHaveBeenCalledTimes(1)
  expect(api.account).toHaveBeenCalledTimes(1)
  api.account.mockResolvedValue(newer)
  await wrapper.get('[data-testid=reload-bps-state]').trigger('click'); await flushPromises()
  expect(api.updateAccount).toHaveBeenCalledTimes(1)
  expect(wrapper.find('[data-testid=bps-state-conflict]').exists()).toBe(false)
  await wrapper.get('[data-testid=enable-bps]').trigger('click'); await flushPromises()
  expect(api.updateAccount).toHaveBeenCalledTimes(2)
  expect(api.updateAccount).toHaveBeenLastCalledWith(12, { bps_enabled: true, bps_expected_disabled_at: newer.excel_bps_403_recovery?.disabled_at }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
})

it.each([{ admin: false, children: true, readOnly: true }, { admin: true, vendorId: 0, readOnly: true }])(
  'shows recovery metadata without granting re-enable or stop in read-only scope %j', async scope => {
    const disabled = disabledAccount()
    api.account.mockResolvedValue(disabled)
    const wrapper = await open(scope)
    expect(wrapper.find('[data-test="bps-failure-badge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid=enable-bps]').exists()).toBe(false)
    expect(wrapper.find('[data-testid=stop-bps-recovery]').exists()).toBe(false)
    expect(api.updateAccount).not.toHaveBeenCalled()
  })
