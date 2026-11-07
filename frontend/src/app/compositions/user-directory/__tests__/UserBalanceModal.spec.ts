import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/features/identity'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserBalanceModal from '../UserBalanceModal.vue'
import { apiClient } from '@/shared/http'
import type { AdminUser } from '@/features/identity/api/profileTypes';
const mocks = vi.hoisted(() => ({ updateBalance: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/features/payments/userBalances', () => ({ updateBalance: mocks.updateBalance }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async importOriginal => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
  setActivePinia(createPinia())
  useAuthStore().user = { id: 99, role: 'super_admin', balance: 100 } as AdminUser
  vi.spyOn(useAuthStore(), 'refreshUser').mockResolvedValue(useAuthStore().user!)
})
afterEach(() => vi.restoreAllMocks())
describe('balance operation error feedback', () => {
  it.each([
    [{ status: 400, message: 'User balance changed; retry' }, 'User balance changed; retry'],
    [{ response: { status: 400, data: { detail: 'Legacy detail' } } }, 'Legacy detail'],
    [{ status: 400 }, 'common.error'],
  ])('shows the available API error and keeps the dialog open', async (error, message) => {
    mocks.updateBalance.mockRejectedValueOnce(error)
    const w = mount(UserBalanceModal, { props: { show: true, operation: 'add', user: { id: 1, email: 'a@example.com', balance: 5 } as AdminUser },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } } })
    await w.get('input[type="number"]').setValue('1')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith(message)
    expect(w.emitted('success')).toBeUndefined(); expect(w.emitted('close')).toBeUndefined()
    expect(w.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
  })

  it.each([
    ['admin', 'add', 6],
    ['super_admin', 'subtract', 6],
  ] as const)('rejects an unfunded %s %s transfer', async (role, operation, amount) => {
    useAuthStore().user = { id: 99, role, balance: 5 } as AdminUser
    const wrapper = mount(UserBalanceModal, {
      props: { show: true, operation, user: { id: 1, email: 'a@example.com', balance: 5 } as AdminUser },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } },
    })
    await wrapper.get('input[type="number"]').setValue(String(amount))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateBalance).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.users.insufficientBalance')
    expect(wrapper.emitted('success')).toBeUndefined()
  })

  it('allows a super-admin deposit beyond personal balance and preserves notes', async () => {
    const auth = useAuthStore()
    auth.user = { id: 99, role: 'super_admin', balance: 0 } as AdminUser
    const refreshUser = vi.spyOn(auth, 'refreshUser').mockResolvedValue(undefined)
    mocks.updateBalance.mockResolvedValueOnce({ id: 1, balance: 12.25 })
    const wrapper = mount(UserBalanceModal, {
      props: { show: true, operation: 'add', user: { id: 1, email: 'a@example.com', balance: 5 } as AdminUser },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } },
    })
    await wrapper.get('input[type="number"]').setValue('7.25')
    await wrapper.get('textarea').setValue('Audited adjustment')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateBalance).toHaveBeenCalledWith(1, 7.25, 'add', 'Audited adjustment')
    expect(refreshUser).toHaveBeenCalledOnce()
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function balanceUser(id = 1, balance = 5) {
  return { id, email: 'user' + id + '@example.test', balance } as AdminUser
}

function mountBalance(operation: 'add' | 'subtract' = 'add', user = balanceUser()) {
  return mount(UserBalanceModal, {
    props: { show: true, operation, user },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } }
  })
}

describe('balance acknowledgement and precision', () => {
  it('uses an id-only restricted acknowledgement solely to request the parent list refresh', async () => {
    const auth = useAuthStore()
    auth.user = { id: 99, role: 'admin', balance: 10 } as AdminUser
    const user = balanceUser()
    mocks.updateBalance.mockResolvedValueOnce({ id: user.id })
    const wrapper = mountBalance('add', user)
    await wrapper.get('input[type="number"]').setValue('1.123456789')
    await wrapper.get('textarea').setValue('  audit note  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateBalance).toHaveBeenCalledWith(1, 1.123456789, 'add', '  audit note  ')
    expect(auth.refreshUser).toHaveBeenCalledOnce()
    expect(wrapper.emitted('success')).toEqual([[]])
    expect(wrapper.emitted('close')).toEqual([[]])
    expect(user.balance).toBe(5)
    expect(auth.user.balance).toBe(10)
  })

  it('withdraws the exact recorded fractional balance without rounding or replacing the row', async () => {
    const user = balanceUser(1, 0.123456789)
    mocks.updateBalance.mockResolvedValueOnce({ id: 1 })
    const wrapper = mountBalance('subtract', user)
    await wrapper.get('button[type="button"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateBalance).toHaveBeenCalledWith(1, 0.123456789, 'subtract', '')
    expect(wrapper.emitted('success')).toEqual([[]])
    expect(user.balance).toBe(0.123456789)
  })

  it('retains zero balances and the existing positive-amount requirement for transfer actions', async () => {
    const wrapper = mountBalance('subtract', balanceUser(1, 0))
    expect(wrapper.text()).toContain('$0.00')
    await wrapper.get('button[type="button"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateBalance).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.users.amountRequired')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('keeps the payment API explicit set-to-zero contract distinct from transfer form validation', async () => {
    const actual = await vi.importActual<typeof import('@/features/payments/userBalances')>('@/features/payments/userBalances')
    const post = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({ data: { id: 1 } })
    await expect(actual.updateBalance(1, 0, 'set', 'zero balance')).resolves.toEqual({ id: 1 })
    expect(post).toHaveBeenCalledOnce()
    expect(post).toHaveBeenCalledWith('/admin/users/1/balance', { balance: 0, operation: 'set', notes: 'zero balance' })
  })
})

describe('balance submission ownership', () => {
  it('blocks duplicate form submission and captures amount, operation and notes once', async () => {
    const command = deferred<{ id: number }>()
    mocks.updateBalance.mockReturnValueOnce(command.promise)
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1.123456789')
    await wrapper.get('textarea').setValue('initial note')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('input[type="number"]').setValue('2')
    await wrapper.get('textarea').setValue('changed note')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateBalance).toHaveBeenCalledOnce()
    expect(mocks.updateBalance).toHaveBeenCalledWith(1, 1.123456789, 'add', 'initial note')
    command.resolve({ id: 1 })
    await flushPromises()
    expect(wrapper.emitted('success')).toEqual([[]])
  })

  it.each(['target', 'actor', 'policy', 'operation', 'round-trip target', 'close', 'unmount'] as const)(
    'ignores an acknowledgement after changing %s', async change => {
      const command = deferred<{ id: number }>()
      mocks.updateBalance.mockReturnValueOnce(command.promise)
      const wrapper = mountBalance()
      await wrapper.get('input[type="number"]').setValue('1')
      await wrapper.get('textarea').setValue('old target note')
      await wrapper.get('form').trigger('submit')
      const auth = useAuthStore()
      if (change === 'target') await wrapper.setProps({ user: balanceUser(2) })
      if (change === 'actor') auth.user = { ...auth.user!, id: 100 }
      if (change === 'policy') auth.user = { ...auth.user!, policy_version: 2 }
      if (change === 'operation') await wrapper.setProps({ operation: 'subtract' })
      if (change === 'round-trip target') {
        await wrapper.setProps({ user: balanceUser(2) })
        await wrapper.setProps({ user: balanceUser(1) })
      }
      if (change === 'close') await wrapper.get('.btn-secondary').trigger('click')
      if (change === 'unmount') wrapper.unmount()
      const closes = wrapper.emitted('close')?.length ?? 0
      command.resolve({ id: 1 })
      await flushPromises()
      expect(auth.refreshUser).not.toHaveBeenCalled()
      expect(mocks.showSuccess).not.toHaveBeenCalled()
      expect(mocks.showError).not.toHaveBeenCalled()
      expect(wrapper.emitted('success')).toBeUndefined()
      expect(wrapper.emitted('close')?.length ?? 0).toBe(closes)
      expect(mocks.updateBalance).toHaveBeenCalledOnce()
    }
  )

  it('checks ownership again after the actor refresh completes', async () => {
    const refresh = deferred<AdminUser>()
    const auth = useAuthStore()
    vi.mocked(auth.refreshUser).mockReturnValueOnce(refresh.promise)
    mocks.updateBalance.mockResolvedValueOnce({ id: 1 })
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(auth.refreshUser).toHaveBeenCalledOnce()
    await wrapper.setProps({ user: balanceUser(2) })
    refresh.resolve({ id: 99, role: 'super_admin', balance: 99 } as AdminUser)
    await flushPromises()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not clear a new target submission when an older target settles', async () => {
    const first = deferred<{ id: number }>()
    const second = deferred<{ id: number }>()
    mocks.updateBalance.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ user: balanceUser(2) })
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    await wrapper.get('input[type="number"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    first.resolve({ id: 1 })
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(useAuthStore().refreshUser).not.toHaveBeenCalled()
    second.resolve({ id: 2 })
    await flushPromises()
    expect(mocks.updateBalance).toHaveBeenNthCalledWith(2, 2, 2, 'add', '')
    expect(wrapper.emitted('success')).toEqual([[]])
  })
})

describe('unknown balance submission outcomes', () => {
  it.each([
    new Error('connection closed after write'),
    { status: 0, code: 'ERR_NETWORK' },
    { status: 500 },
    { response: { status: 503, data: { detail: 'Gateway failure' } } },
    { status: 408 },
    { status: 409, reason: 'IDEMPOTENCY_IN_PROGRESS' },
    { status: 499 },
    { code: 'ERR_CANCELED' },
    {}
  ])('does not permit another submit after ambiguous failure %j', async error => {
    mocks.updateBalance.mockRejectedValueOnce(error)
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('common.unknown')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('input[type="number"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateBalance).toHaveBeenCalledOnce()
    expect(mocks.showError).toHaveBeenCalledOnce()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('retains the unresolved command across close/reopen and target round trips', async () => {
    mocks.updateBalance.mockRejectedValueOnce({ status: 0 })
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.setProps({ show: false, user: null })
    await wrapper.setProps({ show: true, user: balanceUser(1) })
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ user: balanceUser(2) })
    await wrapper.get('input[type="number"]').setValue('1')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ user: balanceUser(1), operation: 'subtract' })
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateBalance).toHaveBeenCalledOnce()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('keeps a late uncertain failure attached to its original target without notifying the new target', async () => {
    const command = deferred<{ id: number }>()
    mocks.updateBalance.mockReturnValueOnce(command.promise)
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ user: balanceUser(2) })
    command.reject({ status: 503, message: 'unknown original outcome' })
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.setProps({ user: balanceUser(1) })
    expect(wrapper.get('[role="alert"]').text()).toContain('unknown original outcome')
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateBalance).toHaveBeenCalledOnce()
  })

  it('permits an explicit corrected submit after a definite server rejection', async () => {
    mocks.updateBalance.mockRejectedValueOnce({ status: 400, message: 'Amount rejected' })
      .mockResolvedValueOnce({ id: 1 })
    const wrapper = mountBalance()
    await wrapper.get('input[type="number"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('input[type="number"]').setValue('1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateBalance).toHaveBeenCalledTimes(2)
    expect(mocks.updateBalance).toHaveBeenNthCalledWith(2, 1, 1, 'add', '')
    expect(wrapper.emitted('success')).toEqual([[]])
  })
})
