import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import BalanceHistoryModal from '../BalanceHistoryModal.vue'
import type { BalanceHistoryPage } from '@/api/user'

const mocks = vi.hoisted(() => ({ getBalanceHistory: vi.fn(), getProfile: vi.fn(), auth: { user: { id: 1 } as { id: number } | null } }))
vi.mock('@/api/user', () => ({ getBalanceHistory: mocks.getBalanceHistory, getProfile: mocks.getProfile }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params?: Record<string, number>) => key + (params ? JSON.stringify(params) : '') }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  mocks.auth = reactive({ user: { id: 1 } })
  mocks.getProfile.mockResolvedValue({ id: 1, email: 'me@example.com', balance: 90 })
  mocks.getBalanceHistory.mockResolvedValue(result())
})
function result(note = 'My adjustment'): BalanceHistoryPage {
  return { items: [{ id: 'credit-1', type: 'admin_balance', value: -10, source: 'admin', notes: note, occurred_at: '2026-10-23T12:00:00Z' }], total: 31, total_recharged: 100 }
}
function deferred() {
  let resolve!: (value: BalanceHistoryPage) => void
  const promise = new Promise<BalanceHistoryPage>(done => { resolve = done })
  return { promise, resolve }
}
function open() {
  return mount(BalanceHistoryModal, { props: { show: true }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } })
}

describe('personal balance history', () => {
  it('shows own account, signed amounts and server pagination, without write actions', async () => {
    const wrapper = open()
    await flushPromises()
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.text()).toContain('me@example.com')
    expect(wrapper.text()).toContain('−$10.00')
    expect(wrapper.text()).toContain('$100.00')
    expect(wrapper.text()).toContain('My adjustment')
    expect(mocks.getBalanceHistory).toHaveBeenCalledWith({ category: 'balance', direction: '', source: '', page: 1, page_size: 15 })
    await wrapper.findAll('button').find(button => button.text() === 'pagination.next')!.trigger('click')
    await flushPromises()
    expect(mocks.getBalanceHistory).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    expect(wrapper.text()).not.toContain('admin.users.deposit')
    expect(wrapper.text()).not.toContain('admin.users.withdraw')
  })

  it('ignores an earlier response after the dialog is reopened', async () => {
    const old = deferred()
    mocks.getBalanceHistory.mockReturnValueOnce(old.promise).mockResolvedValue(result('New result'))
    const wrapper = open()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    old.resolve(result('Stale record'))
    await flushPromises()
    expect(wrapper.text()).toContain('New result')
    expect(wrapper.text()).not.toContain('Stale record')
    expect(mocks.getBalanceHistory).toHaveBeenLastCalledWith(expect.objectContaining({ category: 'balance', page: 1, source: '', direction: '' }))
  })

  it.each([
    ['', ''],
    ['Transfer #4 → #5; ', ''],
    ['Transfer #4 → #5; Customer recharge', 'Customer recharge'],
    ['Transfer #4 → #5; First line\nSecond line', 'First line\nSecond line'],
    ['Customer recharge', 'Customer recharge'],
    ['User-authorized reversal of unfunded admin credit #1', 'User-authorized reversal of unfunded admin credit #1']
  ])('shows only the entered note for %j', async (notes, expected) => {
    mocks.getBalanceHistory.mockResolvedValue(result(notes))
    const wrapper = open()
    await flushPromises()
    expect(wrapper.get('tbody tr').findAll('td')[4].text()).toBe(expected)
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.get('thead').text()).toContain('balanceHistory.amount')
  })

  it('resets pagination when reopened and continues requesting recharge records', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'pagination.next')!.trigger('click')
    await flushPromises()
    expect(mocks.getBalanceHistory).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(mocks.getBalanceHistory).toHaveBeenLastCalledWith({ category: 'balance', direction: '', source: '', page: 1, page_size: 15 })
  })

  it('clears old records on account change and ignores closed requests', async () => {
    const wrapper = open()
    await flushPromises()
    const pending = deferred()
    mocks.getBalanceHistory.mockReturnValueOnce(pending.promise)
    mocks.getProfile.mockResolvedValue({ id: 2, email: 'two@example.com', balance: 0 })
    mocks.auth.user = { id: 2 }
    await flushPromises()
    expect(wrapper.text()).not.toContain('My adjustment')
    expect(wrapper.text()).not.toContain('me@example.com')
    await wrapper.setProps({ show: false })
    pending.resolve(result('Late private record'))
    await flushPromises()
    mocks.getBalanceHistory.mockResolvedValue({ items: [], total: 0, total_recharged: 0 })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Late private record')
    expect(wrapper.text()).toContain('balanceHistory.empty')
  })

  it('shows a retryable failure without presenting stale rows as current', async () => {
    const wrapper = open()
    await flushPromises()
    mocks.getBalanceHistory.mockRejectedValueOnce(new Error('network failure'))
    await wrapper.findAll('button').find(button => button.text() === 'pagination.next')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('balanceHistory.loadFailed')
    expect(wrapper.text()).not.toContain('My adjustment')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('My adjustment')
  })
})
