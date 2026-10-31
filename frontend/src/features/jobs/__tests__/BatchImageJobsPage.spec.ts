import { ref } from 'vue'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BatchImageJobsPage from '../pages/BatchImageJobsPage.vue'
import { jobsServicesKey } from '../services'
import { BATCH_IMAGE_SKILL_NAME } from '@/shared/config/project'
import { createJobsTestServices } from './services'
import type { BatchImageJob } from '../api/batchImage'
const api = vi.hoisted(() => ({
  listBatchImageJobs: vi.fn(), listBatchImageModels: vi.fn(), getBatchImageJob: vi.fn(), listBatchImageItems: vi.fn(),
  cancelBatchImageJob: vi.fn(), deleteBatchImageJobRecord: vi.fn(), downloadBatchImageZip: vi.fn(),
  getBatchImageItemContent: vi.fn(), submitBatchImageJob: vi.fn(), saveBlob: vi.fn(),
}))
vi.mock('../api/batchImage', () => api)
vi.mock('../model/previewThumbnail', () => ({ createPreviewThumbnail: async (blob: Blob) => blob }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ locale: ref('en'), t: (key: string, values?: Record<string, unknown>) => key === 'ui.batchAgentInstruction'
    ? 'name: ' + String(values?.skillName) + '\nsubmit: ' + String(values?.submitUrl) : key }),
}))
const job: BatchImageJob = { id: 'batch-1', object: 'image.batch', task_name: 'Private task A', status: 'running',
  model: 'image-model', provider: 'vertex', item_count: 1, success_count: 0, fail_count: 0,
  estimated_cost: 2, hold_amount: 2, actual_cost: null, created_at: 1700000000, submitted_at: null, settled_at: null }
function mountPage() {
  const scopeKey = ref<string | null>('session-a:user')
  const services = createJobsTestServices({ scopeKey, listPersonalKeys: vi.fn().mockResolvedValue({ items: [
    { id: 7, name: 'Image key', key: 'public-key', status: 'active', group: { name: 'Images', platform: 'gemini', allow_batch_image_generation: true } },
  ], pages: 1 }) })
  const wrapper = shallowMount(BatchImageJobsPage, { global: {
    provide: { [jobsServicesKey as symbol]: services },
    stubs: {
      I18nT: { template: '<span><slot name="count"/><slot name="page"/></span>' },
      TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>' },
      DataTable: { props: ['data'], template: '<div><slot v-for="row in data" name="cell-id" :row="row"/><slot v-for="row in data" name="cell-cost" :row="row"/></div>' },
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' },
    },
  } })
  return { wrapper, services, scopeKey }
}
beforeEach(() => {
  vi.resetAllMocks()
  api.listBatchImageJobs.mockResolvedValue({ data: [job], has_more: false })
  api.listBatchImageModels.mockResolvedValue({ data: [] })
  api.getBatchImageJob.mockResolvedValue(job)
  api.listBatchImageItems.mockResolvedValue({ data: [], has_more: false })
  api.cancelBatchImageJob.mockResolvedValue(job)
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
describe('batch image page lifecycle', () => {
  it.each(['thumbnail', 'dialog'] as const)('recovers a broken %s preview with denied storage and clears displayed URLs on scope change', async target => {
    vi.stubGlobal('indexedDB', { open: () => { throw new DOMException('Storage disabled', 'SecurityError') } })
    let serial = 0
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => `blob:page-${++serial}`), revokeObjectURL: vi.fn() })
    api.listBatchImageItems.mockResolvedValue({ data: [
      { custom_id: 'image-1', status: 'succeeded', image_count: 1, mime_type: 'image/png', file_extension: 'png' },
    ], has_more: false })
    api.getBatchImageItemContent.mockResolvedValue(new Blob(['preview']))
    const { wrapper, services, scopeKey } = mountPage()
    try {
      await flushPromises()
      const select = wrapper.findAll('button').find(button => button.text().includes(job.task_name))
      if (!select) throw new Error('Missing job')
      await select.trigger('click')
      await flushPromises()
      await wrapper.get('button[title="batchImage.detail.previewLoad"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('img').attributes('src')).toBe('blob:page-1')
      await wrapper.get('button[title="batchImage.detail.previewZoom"]').trigger('click')
      expect(wrapper.findAll('img')).toHaveLength(2)
      await wrapper.get(target === 'thumbnail' ? 'img.object-cover' : 'img.object-contain').trigger('error')
      expect(wrapper.findAll('img')).toHaveLength(0)
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:page-1')
      await wrapper.get('button[title="batchImage.detail.previewReload"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('img').attributes('src')).toBe('blob:page-2')
      expect(api.getBatchImageItemContent).toHaveBeenCalledTimes(2)
      expect(api.getBatchImageItemContent).toHaveBeenLastCalledWith('public-key', job.id, 'image-1', 0, expect.any(AbortSignal))
      scopeKey.value = null
      await flushPromises()
      expect(wrapper.findAll('img')).toHaveLength(0)
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:page-2')
      expect(services.notifyError).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it.each([
    { error: { code: 'BATCH_IMAGE_NO_ACCOUNT_AVAILABLE', requestId: 'local-request', status: 503 }, expected: 'batchImage.messages.noCompatibleAccount' },
    { error: new Error('Temporary local failure'), expected: 'Temporary local failure' },
    { error: { code: {}, message: { text: 'not a message' }, requestId: [], status: 'invalid' }, expected: 'batchImage.messages.loadJobsFailed' },
    { error: null, expected: 'batchImage.messages.loadJobsFailed' }
  ])('preserves recognized error semantics and ignores malformed fields: $expected', async ({ error, expected }) => {
    api.listBatchImageJobs.mockRejectedValueOnce(error)
    const { wrapper, services } = mountPage()
    await flushPromises()
    expect(services.notifyError).toHaveBeenCalledWith(expect.stringContaining(expected))
    expect(services.notifyError).not.toHaveBeenCalledWith(expect.stringContaining('[object Object]'))
    wrapper.unmount()
  })

  it('does not let cancellation of the previous job overwrite a newly selected detail', async () => {
    const next: BatchImageJob = { ...job, id: 'batch-2', task_name: 'Private task B' }
    api.listBatchImageJobs.mockResolvedValue({ data: [job, next], has_more: false })
    api.getBatchImageJob.mockImplementation(async (_key: string, id: string) => id === job.id ? job : next)
    let complete: (value: BatchImageJob) => void = () => { throw new Error('No cancellation request') }
    api.cancelBatchImageJob.mockImplementationOnce(() => new Promise<BatchImageJob>(resolve => { complete = resolve }))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, services } = mountPage()
    await flushPromises()
    const first = wrapper.findAll('button').find(button => button.text().includes(job.task_name))
    if (!first) throw new Error('Missing first job')
    await first.trigger('click')
    await flushPromises()
    const cancel = wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.cancelJob')
    if (!cancel) throw new Error('Missing cancellation action')
    await cancel.trigger('click')
    const signal = api.cancelBatchImageJob.mock.calls[0][2]
    const second = wrapper.findAll('button').find(button => button.text().includes(next.task_name))
    if (!second) throw new Error('Missing second job')
    await second.trigger('click')
    await flushPromises()
    complete({ ...job, status: 'cancelled' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').some(button => button.text() === 'batchImage.actions.cancelJob')).toBe(true)
    wrapper.unmount()
  })

  it('does not declare a still-running server job cancelled', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, services } = mountPage()
    await flushPromises()
    const select = wrapper.findAll('button').find(item => item.text().includes(job.task_name))
    expect(select).toBeDefined()
    await select?.trigger('click')
    await flushPromises()
    const cancel = wrapper.findAll('button').find(item => item.text() === 'batchImage.actions.cancelJob')
    expect(cancel).toBeDefined()
    await cancel?.trigger('click')
    await flushPromises()
    expect(api.cancelBatchImageJob).toHaveBeenCalledWith('public-key', 'batch-1', expect.any(AbortSignal))
    expect(services.notifySuccess).not.toHaveBeenCalled()
    const signal = api.cancelBatchImageJob.mock.calls[0][2]
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
  it('clears private projections and rejects a late detail on identity change', async () => {
    let complete: (value: BatchImageJob) => void = () => { throw new Error('No request') }
    api.getBatchImageJob.mockImplementationOnce(() => new Promise<BatchImageJob>(resolve => { complete = resolve }))
    const { wrapper, services, scopeKey } = mountPage()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text().includes(job.task_name))?.trigger('click')
    const signal = api.getBatchImageJob.mock.calls[0][2]
    vi.mocked(services.listPersonalKeys).mockResolvedValue({ items: [], pages: 1 })
    scopeKey.value = 'session-b:user'
    complete(job)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('Private task A')
    expect(services.notifyError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})


describe('generated batch skill identity', () => {
  it('uses the neutral instruction name without adding a backend job field or submitting work', async () => {
    const { wrapper } = mountPage()
    await flushPromises()
    const guide = wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.usageGuide')
    expect(guide).toBeDefined()
    await guide!.trigger('click')
    const instructions = wrapper.findAll('textarea').map(input => (input.element as HTMLTextAreaElement).value)
    expect(instructions.some(value => value.includes('name: ' + BATCH_IMAGE_SKILL_NAME))).toBe(true)
    expect(instructions.join('\n')).toContain('/v1/images/batches')
    expect(instructions.join('\n')).not.toContain('kdan-batch-image')
    expect(api.submitBatchImageJob).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
