import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CloudReconciliation from '../CloudReconciliation.vue'
const mocks = vi.hoisted(() => ({ auth: { isSuperAdmin: true }, list: vi.fn(), reconcile: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/api/admin/accounts', () => ({ getCloudUnsettled: mocks.list, reconcileCloud: mocks.reconcile }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const item = { root_id: 'root', sequence: 3, thread_id: 'thread', turn_id: 'turn', model: 'model', observed_usage: { total_tokens: 12 } }
beforeEach(() => { vi.clearAllMocks(); mocks.auth.isSuperAdmin = true; mocks.list.mockResolvedValue([item]); mocks.reconcile.mockResolvedValue(undefined) })
describe('Cloud verified consumption reconciliation', () => {
 it('restricts the controls to super administrators', () => {
  mocks.auth.isSuperAdmin = false
  const wrapper = mount(CloudReconciliation, { props: { accountId: 1 } })
  expect(wrapper.find('details').exists()).toBe(false)
  expect(mocks.list).not.toHaveBeenCalled()
 })
 it('requires evidence and explicit confirmation; submits the selected version', async () => {
  const wrapper = mount(CloudReconciliation, { props: { accountId: 1 } })
  await wrapper.get('button').trigger('click'); await flushPromises()
  await wrapper.get('select').setValue('root')
  expect(wrapper.findAll('button')[1].attributes('disabled')).toBeDefined()
  await wrapper.get('textarea').setValue('{"total_tokens":12}')
  const fields = wrapper.findAll('input[type="text"], input:not([type])')
  await fields[0].setValue('upstream evidence'); await fields[1].setValue('verified final bill')
  await wrapper.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button')[1].trigger('click'); await flushPromises()
  expect(mocks.reconcile).toHaveBeenCalledWith(1, { root_id: 'root', sequence: 3, status: 'completed', usage: { total_tokens: 12 }, evidence: 'upstream evidence', reason: 'verified final bill', confirmed: true })
  expect(wrapper.get('[role="status"]').text()).toContain('reconcileAccepted')
  expect(wrapper.find('textarea').exists()).toBe(false)
 })
 it('ignores a previous account response', async () => {
  let resolve!: (items: unknown[]) => void
  mocks.list.mockImplementationOnce(() => new Promise(r => { resolve = r }))
  const wrapper = mount(CloudReconciliation, { props: { accountId: 1 } })
  await wrapper.get('button').trigger('click'); await wrapper.setProps({ accountId: 2 })
  resolve([item]); await flushPromises()
  expect(wrapper.findAll('option')).toHaveLength(1)
  expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
 })
})
