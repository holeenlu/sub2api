import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTelemetryPanel from '../AccountTelemetryPanel.vue'

const mocks = vi.hoisted(() => ({ generate: vi.fn(), get: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ generateAccountTelemetry: mocks.generate, getById: mocks.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))

describe('AccountTelemetryPanel', () => {
 beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks() })
 afterEach(() => { vi.useRealTimers() })
 it('explains automatic generation at creation and offers no sample picker', () => {
  const wrapper = mount(AccountTelemetryPanel)
  expect(wrapper.text()).toContain('admin.accounts.telemetry.createHint')
  expect(wrapper.findAll('button')).toHaveLength(0)
  expect(wrapper.find('[data-testid="account-telemetry-select"]').exists()).toBe(false)
  wrapper.unmount()
 })
 it('generates for the selected account, polls, and shows generated JSON', async () => {
  mocks.generate.mockResolvedValue({ status: 'pending', revision: 'r1' })
  mocks.get.mockResolvedValue({ extra: { account_telemetry: { status: 'ready', revision: 'r1', profile: { source: 'official-cc-init', payload_json: '{"event_type":"ClaudeCodeInternalEvent"}', headers: {} } } } })
  const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42 } })
  await wrapper.get('[data-testid="account-telemetry-generate"]').trigger('click')
  await flushPromises()
  expect(mocks.generate).toHaveBeenCalledWith(42)
  expect(wrapper.get('[data-testid="account-telemetry-generate"]').attributes('disabled')).toBeDefined()
  await vi.advanceTimersByTimeAsync(1000); await flushPromises()
  expect(mocks.get).toHaveBeenCalledWith(42)
  expect(wrapper.text()).toContain('admin.accounts.telemetry.ready')
  expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toContain('ClaudeCodeInternalEvent')
  wrapper.unmount()
 })
 it('cancels polling when the account panel closes', async () => {
  const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: { status: 'pending', revision: 'r1' } } })
  wrapper.unmount(); await vi.advanceTimersByTimeAsync(3000)
  expect(mocks.get).not.toHaveBeenCalled()
 })
})
