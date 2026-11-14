import { usageServicesKey } from '../../services'
import { createUsageTestServices } from '../../__tests__/services'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UsageCleanupDialog from '../UsageCleanupDialog.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'

const { listCleanupTasks, createCleanupTask, cancelCleanupTask, showError } = vi.hoisted(() => ({ listCleanupTasks: vi.fn(), createCleanupTask: vi.fn(), cancelCleanupTask: vi.fn(), showError: vi.fn() }))
vi.mock('@/features/usage/components/UsageFilters.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/features/usage/api/adminUsage', () => ({ adminUsageAPI: { listCleanupTasks, createCleanupTask, cancelCleanupTask } }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); createCleanupTask.mockReset().mockResolvedValue({}); cancelCleanupTask.mockReset().mockResolvedValue({}); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => { vi.restoreAllMocks() })
const page = (number: number) => ({ items: [{ id: number, status: 'succeeded', deleted_rows: number, filters: {} }], total: 15, page: number, page_size: 5 })
async function openDialog() {
  listCleanupTasks.mockResolvedValueOnce(page(1))
  const wrapper = mount(UsageCleanupDialog, {
    props: { show: false, filters: {}, startDate: '2026-09-01', endDate: '2026-09-02' },
    global: { provide: { [usageServicesKey as symbol]: createUsageTestServices({ notifyError: showError }) }, stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' }, UsageFilters: true, ConfirmDialog: true, Pagination: true } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('cleanup task pagination', () => {
  it('does not resubmit an unknown cleanup result after refresh or reopening', async () => {
    const wrapper = await openDialog()
    createCleanupTask.mockRejectedValueOnce(new Error('network outcome unknown'))
    listCleanupTasks.mockResolvedValue(page(1))
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm'); await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('common.unknown')
    expect(wrapper.get('button.btn-danger').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await flushPromises()
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm'); await flushPromises()
    expect(createCleanupTask).toHaveBeenCalledTimes(1)
  })
  it('aborts waiting on hide and discards a late cleanup completion without unlocking replay', async () => {
    const wrapper = await openDialog()
    let complete!: (value: unknown) => void
    createCleanupTask.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    listCleanupTasks.mockResolvedValue(page(1))
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm'); await flushPromises()
    const signal = createCleanupTask.mock.calls[0][1].signal as AbortSignal
    await wrapper.setProps({ show: false }); expect(signal.aborted).toBe(true)
    await wrapper.setProps({ show: true }); await flushPromises()
    const readCount = listCleanupTasks.mock.calls.length
    complete({}); await flushPromises()
    expect(listCleanupTasks).toHaveBeenCalledTimes(readCount)
    expect(wrapper.get('button.btn-danger').attributes('disabled')).toBeDefined()
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm')
    expect(createCleanupTask).toHaveBeenCalledTimes(1)
  })
  it('permits an explicit corrected retry after a definite validation rejection', async () => {
    const wrapper = await openDialog()
    listCleanupTasks.mockResolvedValue(page(1))
    createCleanupTask.mockRejectedValueOnce({ status: 400 })
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm'); await flushPromises()
    expect(wrapper.get('button.btn-danger').attributes('disabled')).toBeUndefined()
    wrapper.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm'); await flushPromises()
    expect(createCleanupTask).toHaveBeenCalledTimes(2)
  })

  it.each(['success', 'failure'])('ignores an older page request that completes with %s', async (outcome) => {
    const wrapper = await openDialog()
    let resolve!: (value: object) => void
    let reject!: (error: Error) => void
    listCleanupTasks.mockImplementationOnce(() => new Promise((res, rej) => { resolve = res; reject = rej }))
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    listCleanupTasks.mockResolvedValueOnce(page(3))
    wrapper.findComponent(Pagination).vm.$emit('update:page', 3)
    await flushPromises()
    if (outcome === 'success') resolve(page(2))
    else reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper.findComponent(Pagination).props('page')).toBe(3)
    expect(wrapper.text()).toContain('#3')
    expect(showError).not.toHaveBeenCalled()
  })

  it('ignores a pending result after the dialog is closed and reopened', async () => {
    const wrapper = await openDialog()
    let resolve!: (value: object) => void
    listCleanupTasks.mockImplementationOnce(() => new Promise(res => { resolve = res }))
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    await wrapper.setProps({ show: false })
    listCleanupTasks.mockResolvedValueOnce(page(1))
    await wrapper.setProps({ show: true })
    await flushPromises()
    resolve(page(2))
    await flushPromises()
    expect(wrapper.findComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.text()).toContain('#1')
  })
})
