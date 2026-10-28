import { keysServicesKey } from '../../services'
import { createKeysTestServices } from '../../__tests__/services'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import KeyDataTransfer from '../KeyDataTransfer.vue'

const { exportData, importData, saveAs, showError, showSuccess } = vi.hoisted(() => ({
  exportData: vi.fn(), importData: vi.fn(), saveAs: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('../../api/keys', () => ({ keysAPI: { exportData, importData } }))
vi.mock('file-saver', () => ({ saveAs }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: object) => `${key} ${JSON.stringify(values ?? {})}` }) }))

const backup = () => ({
  type: 'sub2api-keys', version: 1, exported_at: '2026-10-22T00:00:00Z',
  keys: [{ name: 'client', key: 'sk-private-backup-0123', status: 'inactive', group: null }]
})
const render = (selectedIds: number[] = []) => mount(KeyDataTransfer, {
  props: { selectedIds, filters: { search: 'client', status: 'inactive', group_id: 7 } },
  global: { provide: { [keysServicesKey as symbol]: createKeysTestServices({ notifySuccess: showSuccess, notifyError: showError }) },  stubs: {
    Icon: true,
    ConfirmDialog: {
      props: ['show', 'message'], emits: ['confirm', 'cancel'],
      template: '<div v-if="show" data-test="export-confirm"><p>{{ message }}</p><slot/><button data-test="confirm-export" @click="$emit(\'confirm\')">Confirm</button></div>'
    },
    BaseDialog: {
      props: ['show'], emits: ['close'],
      template: '<div v-if="show"><slot/><slot name="footer"/></div>'
    }
  } }
})

async function choose(wrapper: ReturnType<typeof render>, value: unknown, text?: () => Promise<string>) {
  const file = new File([JSON.stringify(value)], 'keys.json', { type: 'application/json' })
  Object.defineProperty(file, 'text', { value: text ?? (() => Promise.resolve(JSON.stringify(value))) })
  const input = wrapper.get('[data-test="key-data-file"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  exportData.mockResolvedValue(backup())
  importData.mockResolvedValue({ created: 1, skipped: 0, failed: 0, errors: [] })
})

describe('API key backup transfer', () => {
  it.each(['selection', 'filter'] as const)('requires a fresh export confirmation after changing the %s', async change => {
    const wrapper = render([3])
    await wrapper.get('[data-test="export-keys"]').trigger('click')
    if (change === 'selection') await wrapper.setProps({ selectedIds: [9] })
    else await wrapper.setProps({ filters: { search: 'different-owner' } })
    expect(wrapper.find('[data-test="export-confirm"]').exists()).toBe(false)
    expect(exportData).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('aborts and discards an export that completes after its target changes', async () => {
    let finish!: (value: ReturnType<typeof backup>) => void
    exportData.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = render([3])
    await wrapper.get('[data-test="export-keys"]').trigger('click')
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    const signal = exportData.mock.calls[0][2].signal as AbortSignal
    await wrapper.setProps({ selectedIds: [9] })
    finish(backup()); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(saveAs).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="export-keys"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('confirms sensitive export and passes the selection without changing the secret', async () => {
    const wrapper = render([3, 8])
    await wrapper.get('[data-test="export-keys"]').trigger('click')
    expect(exportData).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="export-confirm"]').text()).toContain('keys.transfer.exportWarning')
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(exportData).toHaveBeenCalledWith({ search: 'client', status: 'inactive', group_id: 7 }, [3, 8], expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(saveAs).toHaveBeenCalledOnce()
    const blob = saveAs.mock.calls[0][0] as Blob
    const contents = await new Promise<string>((resolve) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as string)
      reader.readAsText(blob)
    })
    expect(JSON.parse(contents)).toEqual(backup())
    expect(saveAs.mock.calls[0][1]).toMatch(/^api-keys-.*\.json$/)
    wrapper.unmount()
  })

  it('exports filters without a page limit and does not download empty results', async () => {
    const wrapper = render()
    exportData.mockResolvedValue({ ...backup(), keys: [] })
    await wrapper.get('[data-test="export-keys"]').trigger('click')
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(exportData).toHaveBeenCalledWith({ search: 'client', status: 'inactive', group_id: 7 }, [], expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(saveAs).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not download a backup larger than the import size limit', async () => {
    const wrapper = render()
    exportData.mockResolvedValue({ ...backup(), keys: [{ ...backup().keys[0], ip_whitelist: Array(400000).fill('10.0.0.1') }] })
    await wrapper.get('[data-test="export-keys"]').trigger('click')
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(saveAs).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps secrets out of the preview and reports partial success for safe retries', async () => {
    const wrapper = render()
    await wrapper.get('[data-test="import-keys"]').trigger('click')
    await choose(wrapper, backup())
    expect(wrapper.text()).not.toContain('sk-private-backup-0123')
    importData.mockResolvedValueOnce({ created: 1, skipped: 1, failed: 1, errors: [{ index: 3, message: 'No permitted destination group' }] })
    await wrapper.get('[data-test="submit-key-import"]').trigger('click')
    await flushPromises()
    expect(importData).toHaveBeenCalledWith(backup(), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.get('[data-test="key-data-result"]').text()).toContain('No permitted destination group')
    expect(wrapper.get('[data-test="submit-key-import"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="submit-key-import"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="submit-key-import"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it.each([{ ...backup(), version: 2 }, { ...backup(), type: 'sub2api-data' }, { ...backup(), keys: [] }, { ...backup(), keys: Array(1001).fill({}) }])('rejects invalid file headers before submission', async (value) => {
    const wrapper = render()
    await wrapper.get('[data-test="import-keys"]').trigger('click')
    await choose(wrapper, value)
    expect(wrapper.get('[role="alert"]').text()).toContain('keys.transfer.invalidFile')
    expect(wrapper.get('[data-test="submit-key-import"]').attributes('disabled')).toBeDefined()
    expect(importData).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('ignores an older file read that finishes after a new selection', async () => {
    const wrapper = render()
    await wrapper.get('[data-test="import-keys"]').trigger('click')
    let finish!: (value: string) => void
    await choose(wrapper, backup(), () => new Promise((resolve) => { finish = resolve }))
    const latest = { ...backup(), keys: [{ ...backup().keys[0], key: 'sk-latest-selection-0123' }] }
    await choose(wrapper, latest)
    finish(JSON.stringify(backup()))
    await flushPromises()
    await wrapper.get('[data-test="submit-key-import"]').trigger('click')
    await flushPromises()
    expect(importData).toHaveBeenCalledWith(latest, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    wrapper.unmount()
  })

  it('keeps the file retryable after a transport failure', async () => {
    const wrapper = render()
    await wrapper.get('[data-test="import-keys"]').trigger('click')
    await choose(wrapper, backup())
    importData.mockRejectedValueOnce(new Error('network unavailable'))
    await wrapper.get('[data-test="submit-key-import"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('keys.transfer.importFailed')
    expect(wrapper.get('[data-test="submit-key-import"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('imported')).toBeUndefined()
    wrapper.unmount()
  })
})
