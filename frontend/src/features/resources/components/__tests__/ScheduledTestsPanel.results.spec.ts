import { managementProvide } from './managementTestServices'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

import ScheduledTestsPanel from '../ScheduledTestsPanel.vue'
import type { ScheduledTestPlan } from '../../api/scheduledTestTypes'
const { listByAccount, listResults, showError, createPlan, updatePlan, deletePlan } = vi.hoisted(() => ({
  listByAccount: vi.fn(), listResults: vi.fn(), showError: vi.fn(),
  createPlan: vi.fn(), updatePlan: vi.fn(), deletePlan: vi.fn(),
}))
vi.mock('@/features/resources/scheduledTests', () => ({ default: { listByAccount, listResults, create: createPlan, update: updatePlan, delete: deletePlan } }))


const plan = (id: number): ScheduledTestPlan => ({
  id, account_id: 1, model_id: 'model-' + id, cron_expression: '*/30 * * * *',
  enabled: true, max_results: 100, auto_recover: false, last_run_at: null, next_run_at: null,
  created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z'
})

function readSignal(call: readonly unknown[]): AbortSignal {
  const options = call[call.length - 1]
  if (typeof options !== 'object' || options === null || !('signal' in options)
    || !(options.signal instanceof AbortSignal)) throw new Error('Missing request AbortSignal')
  return options.signal
}

async function openPanel() {
  const wrapper = shallowMount(ScheduledTestsPanel, {
    props: { show: false, accountId: 1, modelOptions: [] },
    global: { provide: managementProvide({ showError }), stubs: { BaseDialog: { name: 'BaseDialog', emits: ['close'], template: '<div><slot /></div>' } } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}
const result = (id: number) => ({ id, latency_ms: id, status: 'success', started_at: '2026-10-03T00:00:00Z' })

describe('scheduled test result selection', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listByAccount.mockResolvedValue([plan(1), plan(2)])
    createPlan.mockResolvedValue(plan(3))
    deletePlan.mockResolvedValue(undefined)
  })

  it.each(['resolve', 'reject'] as const)('ignores an old plan request that later %ss', async outcome => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const headers = wrapper.findAll('.cursor-pointer')
    await headers[0].trigger('click')
    await headers[1].trigger('click')
    await flushPromises()
    const current = wrapper.html()
    if (outcome === 'resolve') old.resolve([result(11)])
    else old.reject(new Error('obsolete error'))
    await flushPromises()
    expect(wrapper.html()).toBe(current)
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps loading the current plan when the previous request completes', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    const current = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openPanel()
    await wrapper.findAll('.cursor-pointer')[0].trigger('click')
    await wrapper.findAll('.cursor-pointer')[1].trigger('click')
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.text()).toContain('common.loading')
    current.resolve([result(22)])
    await flushPromises()
    expect(wrapper.text()).not.toContain('common.loading')
  })

  it('invalidates a request when the same plan is collapsed and reopened', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const header = wrapper.findAll('.cursor-pointer')[0]
    await header.trigger('click')
    await header.trigger('click')
    await header.trigger('click')
    await flushPromises()
    const current = wrapper.html()
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.html()).toBe(current)
  })
  it('keeps a newly opened editor when an earlier plan save finishes', async () => {
    const old = deferred<ScheduledTestPlan>()
    updatePlan.mockReturnValueOnce(old.promise)
    const wrapper = await openPanel()
    const edit = wrapper.findAll('button[title="admin.scheduledTests.editPlan"]')
    await edit[0].trigger('click')
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    if (!save) throw new Error('Missing edit save button')
    await save.trigger('click')
    expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({ model_id: 'model-1' }), { signal: expect.any(AbortSignal) })
    await edit[1].trigger('click')
    expect(readSignal(updatePlan.mock.calls[0]).aborted).toBe(true)
    old.resolve({ ...plan(1), cron_expression: '0 * * * *' })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'Select' }).exists()).toBe(true)
    expect(wrapper.getComponent({ name: 'Select' }).props('modelValue')).toBe('model-2')
  })

  it('does not close or retarget a later delete confirmation', async () => {
    const old = deferred<void>()
    deletePlan.mockReturnValueOnce(old.promise)
    const wrapper = await openPanel()
    const buttons = wrapper.findAll('button[title="admin.scheduledTests.deletePlan"]')
    await buttons[0].trigger('click')
    wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(deletePlan).toHaveBeenCalledWith(1, { signal: expect.any(AbortSignal) })
    await buttons[1].trigger('click')
    old.resolve()
    await flushPromises()
    const confirm = wrapper.getComponent({ name: 'ConfirmDialog' })
    expect(confirm.props('show')).toBe(true)
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(deletePlan.mock.calls.map(call => call[0])).toEqual([1, 2])
  })

  it('aborts superseded result reads as well as rejecting their late values', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const headers = wrapper.findAll('.cursor-pointer')
    await headers[0].trigger('click')
    const signal = readSignal(listResults.mock.calls[0])
    await headers[1].trigger('click')
    expect(signal.aborted).toBe(true)
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.text()).not.toContain('11ms')
  })

  it('keeps the newest plan list after a create reload overtakes the initial read', async () => {
    const initial = deferred<ScheduledTestPlan[]>()
    listByAccount.mockReturnValueOnce(initial.promise).mockResolvedValueOnce([plan(3)])
    const wrapper = await openPanel()
    await wrapper.get('button').trigger('click')
    wrapper.getComponent({ name: 'Select' }).vm.$emit('update:modelValue', 'model-3')
    wrapper.findAllComponents({ name: 'Input' })[0].vm.$emit('update:modelValue', '*/30 * * * *')
    await wrapper.vm.$nextTick()
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    if (!save) throw new Error('Missing create save button')
    await save.trigger('click')
    await flushPromises()
    expect(createPlan).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('model-3')
    initial.resolve([plan(1)])
    await flushPromises()
    expect(wrapper.text()).toContain('model-3')
    expect(wrapper.text()).not.toContain('model-1')
  })

  it.each(['target', 'close', 'unmount'] as const)('aborts an in-flight plan save on %s and discards its result', async boundary => {
    const pending = deferred<ScheduledTestPlan>()
    updatePlan.mockReturnValueOnce(pending.promise)
    const wrapper = await openPanel()
    await wrapper.findAll('button[title="admin.scheduledTests.editPlan"]')[0].trigger('click')
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    if (!save) throw new Error('Missing save')
    await save.trigger('click')
    const signal = readSignal(updatePlan.mock.calls[0])
    if (boundary === 'target') await wrapper.setProps({ accountId: 2 })
    else if (boundary === 'close') wrapper.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
    else wrapper.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve({ ...plan(1), model_id: 'late-model' })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    if (boundary !== 'unmount') expect(wrapper.text()).not.toContain('late-model')
  })

})
