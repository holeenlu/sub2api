import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import AccountPriorityCell from '../AccountPriorityCell.vue'
import type { Account } from '@/features/resources/api/accountTypes';
import { update } from '@/features/resources/accounts'

enableAutoUnmount(afterEach)

vi.mock('@/features/resources/accounts', () => ({ update: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const account = (overrides: Partial<Account> = {}) => ({
  id: 7, name: 'Claude 1', platform: 'anthropic', type: 'oauth', priority: 3,
  ...overrides,
}) as Account

const mountCell = (value = account()) => mount(AccountPriorityCell, { props: { account: value, scopeKey: () => 'actor-a', canEdit: () => true } })

beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(update).mockReset().mockImplementation(async (id, req) => account({ id, priority: req.priority }))
})
afterEach(() => {
  vi.useRealTimers()
})

describe('AccountPriorityCell', () => {
  it('batches rapid +/- clicks into a single priority-only update', async () => {
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await wrapper.get('[data-testid="account-priority-decrement"]').trigger('click')
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('4')
    expect(update).not.toHaveBeenCalled()

    await vi.runAllTimersAsync()
    await flushPromises()

    expect(update).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledWith(7, { priority: 4 }, { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ id: 7, priority: 4 })
  })

  it('does not go below 1', async () => {
    const wrapper = mountCell(account({ priority: 1 }))
    const dec = wrapper.get('[data-testid="account-priority-decrement"]')
    expect(dec.attributes('disabled')).toBeDefined()
    // 到达下限时按钮仍应随悬停显隐，而不是常驻半透明
    expect(dec.classes()).toContain('opacity-0')
    expect(dec.classes().some(c => c.startsWith('disabled:opacity'))).toBe(false)
    await dec.trigger('click')
    await vi.runAllTimersAsync()
    expect(update).not.toHaveBeenCalled()
  })

  it('saves a typed value on Enter and ignores unchanged input', async () => {
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-value"]').trigger('click')
    const input = wrapper.get('[data-testid="account-priority-input"]')
    await input.setValue('12')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { priority: 12 }, { signal: expect.any(AbortSignal) })

    vi.mocked(update).mockClear()
    await wrapper.setProps({ account: account({ priority: 12 }) })
    await wrapper.get('[data-testid="account-priority-value"]').trigger('click')
    await wrapper.get('[data-testid="account-priority-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(update).not.toHaveBeenCalled()
  })

  it('Escape cancels typing without saving', async () => {
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-value"]').trigger('click')
    const input = wrapper.get('[data-testid="account-priority-input"]')
    await input.setValue('50')
    await input.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(update).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('3')
  })

  it('reports an unknown response without replaying on unmount', async () => {
    vi.mocked(update).mockRejectedValueOnce(new Error('response outcome unknown'))
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.runAllTimersAsync()
    await flushPromises()
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('3')
    expect(wrapper.emitted('error')).toHaveLength(1)
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(update).toHaveBeenCalledOnce()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(1000)
    expect(update).toHaveBeenCalledOnce()
  })
  it('keeps priority readable but prevents every edit when the field is denied', async () => {
    const wrapper = mount(AccountPriorityCell, { props: {
      account: account(), scopeKey: () => 'actor-a', canEdit: () => false
    } })
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('3')
    for (const key of ['value', 'increment', 'decrement']) {
      const control = wrapper.get('[data-testid="account-priority-' + key + '"]')
      expect(control.attributes('disabled')).toBeDefined()
      await control.trigger('click')
    }
    await vi.advanceTimersByTimeAsync(1000)
    expect(update).not.toHaveBeenCalled()
  })

  it.each(['actor', 'field', 'target', 'unmount'] as const)('discards a queued priority change on %s without dispatch', async boundary => {
    const scope = ref('actor-a')
    const allowed = ref(true)
    const wrapper = mount(AccountPriorityCell, { props: {
      account: account(), scopeKey: () => scope.value, canEdit: () => allowed.value
    } })
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    if (boundary === 'actor') scope.value = 'actor-b'
    else if (boundary === 'field') allowed.value = false
    else if (boundary === 'target') await wrapper.setProps({ account: account({ id: 8, priority: 30 }) })
    else wrapper.unmount()
    await vi.advanceTimersByTimeAsync(1000)
    expect(update).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it.each(['actor', 'field', 'target', 'unmount'] as const)('aborts HTTP and rejects late priority results after %s', async boundary => {
    const scope = ref('actor-a')
    const allowed = ref(true)
    let finish: (value: Account) => void = () => { throw new Error('Not initialized') }
    vi.mocked(update).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(AccountPriorityCell, { props: {
      account: account(), scopeKey: () => scope.value, canEdit: () => allowed.value
    } })
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.advanceTimersByTimeAsync(450)
    const options = vi.mocked(update).mock.calls[0][2]
    expect(options?.signal).toBeInstanceOf(AbortSignal)
    if (boundary === 'actor') scope.value = 'actor-b'
    else if (boundary === 'field') allowed.value = false
    else if (boundary === 'target') await wrapper.setProps({ account: account({ id: 8, priority: 30 }) })
    else wrapper.unmount()
    expect(options?.signal?.aborted).toBe(true)
    finish(account({ priority: 4 }))
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('error')).toBeUndefined()
  })

  it('keeps the clicked value when the same target refreshes before debounce completes', async () => {
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await wrapper.setProps({ account: account({ priority: 20 }) })
    await vi.advanceTimersByTimeAsync(450)
    expect(update).toHaveBeenCalledWith(7, { priority: 4 }, { signal: expect.any(AbortSignal) })
  })

  it.each([
    // app/http/install throws this shape when requestAdminStepUp resolves false.
    Object.assign(new Error('step-up verification cancelled by user'), {
      name: 'StepUpCancelledError', code: 'STEP_UP_CANCELLED'
    }),
    Object.assign(new Error('canceled'), { name: 'CanceledError', code: 'ERR_CANCELED' })
  ])('silently resets a current priority draft for global cancellation $code', async error => {
    vi.mocked(update).mockRejectedValueOnce(error)
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.advanceTimersByTimeAsync(450)
    await flushPromises()

    // Global dialog cancellation does not abort the component-owned controller.
    expect(vi.mocked(update).mock.calls[0][2]?.signal?.aborted).toBe(false)
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('3')
    expect(wrapper.find('[data-testid="account-priority-saving"]').exists()).toBe(false)
    expect(wrapper.emitted('error')).toBeUndefined()
    expect(wrapper.emitted('updated')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(1000)
    expect(update).toHaveBeenCalledOnce()

    // A new explicit user intent is still allowed after the cancelled attempt.
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.advanceTimersByTimeAsync(450)
    await flushPromises()
    expect(update).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ id: 7, priority: 4 })
  })

  it.each([
    { code: 'PERMISSION_DENIED', message: 'Priority permission denied' },
    { code: 'STEP_UP_TOTP_NOT_ENABLED', message: 'Step-up is unavailable' },
    { code: 'ERR_NETWORK', message: 'step-up verification cancelled by user' }
  ])('preserves a genuine failure message for $code', async error => {
    vi.mocked(update).mockRejectedValueOnce(error)
    const wrapper = mountCell()
    await wrapper.get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.advanceTimersByTimeAsync(450)
    await flushPromises()
    expect(wrapper.get('[data-testid="account-priority-value"]').text()).toBe('3')
    expect(wrapper.emitted('error')).toEqual([[error.message]])
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

})
