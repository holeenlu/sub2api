import { ref } from 'vue'
import { flushPromises, shallowMount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BatchImageJobsPage from '../../pages/BatchImageJobsPage.vue'
import BatchImagePromptDraftPanel from '../BatchImagePromptDraftPanel.vue'
import { jobsServicesKey } from '../../services'
import { createJobsTestServices } from '../../__tests__/services'
import type { BatchImageJob } from '../../api/batchImage'

const api = vi.hoisted(() => ({
  listBatchImageJobs: vi.fn(), listBatchImageModels: vi.fn(), getBatchImageJob: vi.fn(),
  listBatchImageItems: vi.fn(), cancelBatchImageJob: vi.fn(), deleteBatchImageJobRecord: vi.fn(),
  downloadBatchImageZip: vi.fn(), getBatchImageItemContent: vi.fn(), submitBatchImageJob: vi.fn(), saveBlob: vi.fn(),
}))
vi.mock('../../api/batchImage', () => api)
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale: ref('en'), t: (key: string, values?: Record<string, unknown>) =>
    key === 'batchImage.detail.holdCost' ? `${key}: ${values?.amount}` : key }),
}))

const job: BatchImageJob = {
  id: 'batch-draft', object: 'image.batch', task_name: 'Draft task', status: 'running',
  model: 'gemini-flash-image', provider: 'vertex', item_count: 1, success_count: 0, fail_count: 0,
  estimated_cost: 2, hold_amount: 2, actual_cost: null, created_at: 1700000000, submitted_at: null, settled_at: null,
}

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function button(wrapper: Pick<VueWrapper, 'findAll'>, text: string) {
  const found = wrapper.findAll('button').find(item => item.text() === text)
  if (!found) throw new Error(`Missing button ${text}`)
  return found
}

const mounted: VueWrapper[] = []
function mountPage() {
  const scopeKey = ref<string | null>('session-a:user')
  const services = createJobsTestServices({ scopeKey, listPersonalKeys: vi.fn().mockResolvedValue({ items: [
    { id: 7, name: 'Image key', key: 'generation-key', status: 'active', group: { name: 'Images', platform: 'gemini', allow_batch_image_generation: true } },
  ], pages: 1 }) })
  const wrapper = shallowMount(BatchImageJobsPage, { global: {
    provide: { [jobsServicesKey as symbol]: services },
    stubs: {
      BatchImagePromptDraftPanel: false,
      I18nT: { template: '<span><slot name="count"/><slot name="page"/></span>' },
      TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>' },
      DataTable: { props: ['data'], template: '<div><slot name="header-select"/><div v-for="row in data"><slot name="cell-select" :row="row"/><slot name="cell-id" :row="row"/><slot name="cell-cost" :row="row"/></div></div>' },
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' },
    },
  } })
  mounted.push(wrapper)
  return { wrapper, services, scopeKey }
}

async function openCreate(wrapper: VueWrapper) {
  await flushPromises()
  await button(wrapper, 'batchImage.actions.createJob').trigger('click')
  return wrapper.getComponent(BatchImagePromptDraftPanel)
}

async function chooseFiles(wrapper: VueWrapper, files: File[]) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: files })
  await input.trigger('change')
  await vi.waitFor(() => expect(wrapper.getComponent(BatchImagePromptDraftPanel).props('referenceNames').length).toBeGreaterThan(0))
}

beforeEach(() => {
  vi.resetAllMocks()
  api.listBatchImageJobs.mockResolvedValue({ data: [job], has_more: false })
  api.listBatchImageModels.mockResolvedValue({ data: [{ id: job.model }] })
  api.getBatchImageJob.mockResolvedValue(job)
  api.listBatchImageItems.mockResolvedValue({ data: [], has_more: false })
  api.submitBatchImageJob.mockResolvedValue(job)
})
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('batch image page consumes the prompt draft panel', () => {
  it('keeps row normalization, file reading and the single request submission in the parent', async () => {
    const { wrapper } = mountPage()
    const panel = await openCreate(wrapper)
    await wrapper.get('input[maxlength="255"]').setValue('  Created task  ')
    await panel.get('textarea').setValue('  First scene  ')
    await panel.get('input[type="text"]').setValue('same id')
    await panel.get('select').setValue('3')
    await chooseFiles(wrapper, [new File(['reference'], 'face.png', { type: 'image/png' })])
    await button(panel, 'common.add').trigger('click')
    expect(panel.props()).toMatchObject({ prompt: '', customId: '', outputCount: 1, referenceNames: [], rows: [
      { customId: 'same_id', prompt: 'First scene', outputCount: 3, referenceCount: 1 },
    ] })
    expect(JSON.stringify(panel.props())).not.toContain('cmVmZXJlbmNl')
    expect(JSON.stringify(panel.props())).not.toContain('generation-key')
    await panel.get('textarea').setValue('  Second scene  ')
    await panel.get('input[type="text"]').setValue('same id')
    expect(api.submitBatchImageJob).not.toHaveBeenCalled()
    await button(wrapper, 'batchImage.actions.submitJob').trigger('click')
    await flushPromises()
    expect(api.submitBatchImageJob).toHaveBeenCalledTimes(1)
    expect(api.submitBatchImageJob).toHaveBeenCalledWith('generation-key', {
      model: job.model, task_name: 'Created task', image_size: '1K', response_mime_type: 'image/png',
      items: [
        { custom_id: 'same_id', prompt: 'First scene', output_count: 3, reference_images: [
          { id: 'face.png', type: 'reference', mime_type: 'image/png', data: 'cmVmZXJlbmNl' },
        ] },
        { custom_id: 'same_id_2', prompt: 'Second scene' },
      ],
    }, expect.stringMatching(/^sub2api-ui-/), expect.any(AbortSignal))
    expect(wrapper.findComponent(BatchImagePromptDraftPanel).exists()).toBe(false)
    expect(wrapper.text()).toContain('batchImage.detail.holdCost: $2.00')
    expect(api.listBatchImageItems).toHaveBeenCalledWith('generation-key', job.id, '', expect.any(AbortSignal))
  })

  it('removes the intended prompt and reference, and clears all draft UI on cancel', async () => {
    const { wrapper } = mountPage()
    const panel = await openCreate(wrapper)
    for (const prompt of ['First', 'Second']) {
      await panel.get('textarea').setValue(prompt)
      await button(panel, 'common.add').trigger('click')
    }
    await panel.findAll('button[title="common.delete"]')[0].trigger('click')
    expect(panel.props('rows')).toMatchObject([{ prompt: 'Second' }])
    await chooseFiles(wrapper, [new File(['image'], 'scene.png', { type: 'image/png' })])
    await panel.get('button[title="batchImage.create.removeReferenceImage"]').trigger('click')
    expect(panel.props('referenceNames')).toEqual([])
    await panel.get('textarea').setValue('Unadded draft')
    await button(wrapper, 'common.cancel').trigger('click')
    const reopened = await openCreate(wrapper)
    expect(reopened.props()).toMatchObject({ prompt: '', customId: '', outputCount: 1, referenceNames: [], rows: [] })
    expect(api.submitBatchImageJob).not.toHaveBeenCalled()
  })

  it('keeps file validation and model reference limits in the parent', async () => {
    const { wrapper, services } = mountPage()
    const panel = await openCreate(wrapper)
    const oversized = new File(['image'], 'too-large.png', { type: 'image/png' })
    Object.defineProperty(oversized, 'size', { value: 10 * 1024 * 1024 + 1 })
    await chooseFiles(wrapper, [
      new File(['text'], 'notes.txt', { type: 'text/plain' }), oversized,
      new File(['image'], 'valid.webp', { type: 'image/webp' }),
      new File(['ignored'], 'beyond-limit.png', { type: 'image/png' }),
    ])
    expect(services.notifyError).toHaveBeenCalledWith('batchImage.create.refLimitExceededIgnored')
    expect(services.notifyError).toHaveBeenCalledWith('batchImage.create.refFormatUnsupported')
    expect(services.notifyError).toHaveBeenCalledWith('batchImage.create.refFileTooLarge')
    expect(panel.props('referenceNames')).toEqual(['valid.webp'])
    expect(api.submitBatchImageJob).not.toHaveBeenCalled()
  })

  it('retains the draft after an unknown submit result and does not automatically submit again', async () => {
    api.submitBatchImageJob.mockRejectedValueOnce(new Error('Submission status unknown'))
    const { wrapper, services } = mountPage()
    const panel = await openCreate(wrapper)
    await panel.get('textarea').setValue('Keep this prompt')
    await button(wrapper, 'batchImage.actions.submitJob').trigger('click')
    await flushPromises()
    expect(services.notifyError).toHaveBeenCalledWith('Submission status unknown')
    expect(panel.props('rows')).toMatchObject([{ prompt: 'Keep this prompt' }])
    expect(button(wrapper, 'batchImage.actions.submitJob').attributes('disabled')).toBeUndefined()
    expect(api.submitBatchImageJob).toHaveBeenCalledTimes(1)
    expect(services.notifySuccess).not.toHaveBeenCalled()
  })

  it('aborts a pending file read on identity change and rejects its late draft data', async () => {
    const readers: FileReader[] = []
    vi.spyOn(FileReader.prototype, 'readAsDataURL').mockImplementation(function (this: FileReader) {
      readers.push(this)
    })
    const abort = vi.spyOn(FileReader.prototype, 'abort')
    const { wrapper, services, scopeKey } = mountPage()
    const panel = await openCreate(wrapper)
    const input = panel.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['private'], 'private.png', { type: 'image/png' })] })
    await input.trigger('change')
    expect(readers).toHaveLength(1)
    scopeKey.value = 'session-b:user'
    await flushPromises()
    expect(abort).toHaveBeenCalledTimes(1)
    const reader = readers[0]
    Object.defineProperty(reader, 'result', { value: 'data:image/png;base64,cHJpdmF0ZQ==' })
    reader.dispatchEvent(new ProgressEvent('load'))
    await flushPromises()
    const reopened = await openCreate(wrapper)
    expect(reopened.props()).toMatchObject({ prompt: '', rows: [], referenceNames: [] })
    expect(services.notifyError).not.toHaveBeenCalled()
    expect(api.submitBatchImageJob).not.toHaveBeenCalled()
  })

  it('blocks duplicate submits and rejects a late result when the parent session changes', async () => {
    const pending = deferred<BatchImageJob>()
    api.submitBatchImageJob.mockReturnValueOnce(pending.promise)
    const { wrapper, services, scopeKey } = mountPage()
    const panel = await openCreate(wrapper)
    await panel.get('textarea').setValue('Private draft')
    const submit = button(wrapper, 'batchImage.actions.submitJob')
    await submit.trigger('click')
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click')
    expect(api.submitBatchImageJob).toHaveBeenCalledTimes(1)
    const signal = api.submitBatchImageJob.mock.calls[0][3]
    scopeKey.value = null
    await flushPromises()
    expect(signal.aborted).toBe(true)
    pending.resolve(job)
    await flushPromises()
    expect(wrapper.findComponent(BatchImagePromptDraftPanel).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Private draft')
    expect(wrapper.text()).not.toContain(job.task_name)
    expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(services.notifyError).not.toHaveBeenCalled()
    expect(api.listBatchImageItems).not.toHaveBeenCalled()
  })

  it('keeps selected downloads limited to eligible jobs and records only a completed download', async () => {
    const completed: BatchImageJob = { ...job, id: 'complete', task_name: 'Completed', status: 'completed', success_count: 1 }
    api.listBatchImageJobs.mockResolvedValue({ data: [job, completed], has_more: false })
    const pending = deferred<Blob>()
    api.downloadBatchImageZip.mockReturnValueOnce(pending.promise)
    const { wrapper } = mountPage()
    await flushPromises()
    await wrapper.findAll('input[type="checkbox"]')[0].setValue(true)
    const download = button(wrapper, 'batchImage.actions.downloadSelected')
    await download.trigger('click')
    expect(download.attributes('disabled')).toBeDefined()
    expect(api.downloadBatchImageZip).toHaveBeenCalledTimes(1)
    expect(api.downloadBatchImageZip).toHaveBeenCalledWith('generation-key', completed.id, expect.any(AbortSignal))
    expect(api.saveBlob).not.toHaveBeenCalled()
    const blob = new Blob(['zip'])
    pending.resolve(blob)
    await flushPromises()
    expect(api.saveBlob).toHaveBeenCalledWith(blob, 'complete.zip')
  })
})
