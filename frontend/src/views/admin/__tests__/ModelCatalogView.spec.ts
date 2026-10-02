import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import ModelCatalogView from '../ModelCatalogView.vue'

const mocks = vi.hoisted(() => ({ getModelCatalog: vi.fn(), getCatalogSettings: vi.fn(), saveCatalogSettings: vi.fn(), syncModelCatalog: vi.fn(), saveCatalogModel: vi.fn() }))
const groupMocks = vi.hoisted(() => ({ getAll: vi.fn(), update: vi.fn() }))
vi.mock('@/api/admin/modelCatalog', () => mocks)
vi.mock('@/api/admin', () => ({ adminAPI: { groups: groupMocks } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
const Select = defineComponent({ props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>` })
const render = () => mount(ModelCatalogView, { global: { stubs: { Select, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, Pagination: true } } })
const models = [
  { id: 'new-chat', platform: 'openai', kind: 'chat', display_name: 'New chat', lifecycle: 'active' },
  { id: 'new-image', platform: 'openai', kind: 'image', lifecycle: 'active' },
  { id: 'new-video', platform: 'gemini', kind: 'video', lifecycle: 'active' },
  { id: 'disabled-model', platform: 'anthropic', kind: 'chat', disabled: true, lifecycle: 'active' }
]
const button = (wrapper: ReturnType<typeof render>, key: string) => wrapper.findAll('button').find(item => item.text() === key)!
beforeEach(() => {
  vi.clearAllMocks()
  mocks.getCatalogSettings.mockResolvedValue({ enabled: false, interval_seconds: 300 })
  mocks.getModelCatalog.mockResolvedValue({ models, status: 'ready', revision: 'one' })
  mocks.syncModelCatalog.mockResolvedValue({ status: 'complete', succeeded: 2, failed: 0 })
  mocks.saveCatalogModel.mockResolvedValue(undefined)
  groupMocks.getAll.mockResolvedValue([])
  groupMocks.update.mockResolvedValue(undefined)
})
describe('model inventory', () => {
  it('lists all platforms and media without permissions, policy or JSON editors', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('Anthropic')
    expect(wrapper.text()).toContain('new-image')
    expect(wrapper.text()).toContain('new-video')
    expect(wrapper.text()).not.toContain('disabled-model')
    expect(wrapper.findAll('textarea')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('modelCatalog.compare')
    expect(wrapper.findAll('select')).toHaveLength(3)
    wrapper.unmount()
  })
  it('filters by platform, kind and search without loading account lists', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.find('#catalog-platform').setValue('openai')
    await wrapper.find('#catalog-kind').setValue('image')
    expect(wrapper.text()).toContain('new-image')
    expect(wrapper.text()).not.toContain('new-video')
    expect(wrapper.text()).not.toContain('new-chat')
    await wrapper.find('input[type="search"]').setValue('absent')
    expect(wrapper.text()).toContain('modelCatalog.empty')
    expect(mocks.getModelCatalog).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('manually syncs while automatic refresh stays disabled', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'modelCatalog.syncNow').trigger('click'); await flushPromises()
    expect(mocks.syncModelCatalog).toHaveBeenCalledTimes(1)
    expect(mocks.saveCatalogSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('modelCatalog.syncSummary')
    wrapper.unmount()
  })
  it('saves a manual media model with no account or pricing changes', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'modelCatalog.addModel').trigger('click')
    await wrapper.find('#model-id').setValue('future-image')
    await wrapper.find('#model-kind').setValue('image')
    await wrapper.findAll('form')[1].trigger('submit'); await flushPromises()
    expect(mocks.saveCatalogModel).toHaveBeenCalledWith({ id: 'future-image', platform: 'openai', kind: 'image', display_name: '', disabled: false })
    expect(wrapper.find('#model-id').exists()).toBe(false)
    wrapper.unmount()
  })
  it('does not overwrite an existing model through the add form', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'modelCatalog.addModel').trigger('click')
    await wrapper.find('#model-id').setValue('new-chat')
    await wrapper.findAll('form')[1].trigger('submit'); await flushPromises()
    expect(mocks.saveCatalogModel).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('modelCatalog.duplicateModel')
    wrapper.unmount()
  })
  it('keeps edits after a save error', async () => {
    mocks.saveCatalogModel.mockRejectedValue(new Error('offline'))
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'modelCatalog.edit').trigger('click')
    expect(wrapper.find('#model-id').attributes('disabled')).toBeDefined()
    await wrapper.find('#model-display-name').setValue('Edited')
    await wrapper.findAll('form')[1].trigger('submit'); await flushPromises()
    expect((wrapper.find('#model-display-name').element as HTMLInputElement).value).toBe('Edited')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('loads the saved fixed source and updates only that group config', async () => {
    groupMocks.getAll.mockResolvedValue([{
      id: 7,
      name: 'Codex source',
      codex_models_manifest_config: { enabled: true, account_ids: [11, 12], fallback_to_scheduler: true }
    }])
    groupMocks.update.mockResolvedValue({
      id: 7,
      name: 'Codex source',
      codex_models_manifest_config: { enabled: true, account_ids: [11, 12], fallback_to_scheduler: true }
    })
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('#catalog-group').exists()).toBe(true)
    expect(wrapper.text()).toContain('#11')
    expect(wrapper.text()).toContain('#12')
    await button(wrapper, 'modelCatalog.saveCodexSource').trigger('click'); await flushPromises()
    expect(groupMocks.update).toHaveBeenCalledWith(7, {
      codex_models_manifest_config: { enabled: true, account_ids: [11, 12], fallback_to_scheduler: true }
    }, { signal: expect.any(AbortSignal) })
    wrapper.unmount()
  })
})
