import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

const { getModelCatalog, refreshModelCatalog } = vi.hoisted(() => ({ getModelCatalog: vi.fn(), refreshModelCatalog: vi.fn() }))
vi.mock('@/api/admin/modelCatalog', () => ({ getModelCatalog, refreshModelCatalog }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showInfo: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function entry(id: string, lifecycle = 'active', access = 'listed') {
  return { id, display_name: id, platform: 'openai', kind: 'chat', lifecycle, access, source: 'upstream', metadata: { id }, missing: [], endpoints: [] }
}
function snapshot(models: ReturnType<typeof entry>[]) { return { revision: 'version-one', status: 'ready', models } }
function mountEditor(value: string[] = []) {
  return mount(ModelWhitelistSelector, { props: { platform: 'openai', accountId: 17, modelValue: value }, global: { stubs: { ModelIcon: true, Icon: true } } })
}
async function click(wrapper: ReturnType<typeof mountEditor>, label: string) {
  await wrapper.findAll('button').find(b => b.text() === label)!.trigger('click')
  await flushPromises()
}

describe('dynamic model selector', () => {
  beforeEach(() => {
    getModelCatalog.mockReset().mockResolvedValue(snapshot([entry('opaque-future-from-upstream')]))
    refreshModelCatalog.mockReset()
  })

  it('offers an ID not compiled into presets and does not select it merely by loading', async () => {
    const wrapper = mountEditor(['manual-alias'])
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.text()).toContain('opaque-future-from-upstream')
    expect(wrapper.text()).not.toContain('gpt-5.2')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(getModelCatalog).toHaveBeenCalledWith({ account_id: 17, view: 'selection' }, expect.any(AbortSignal))
    await click(wrapper, 'modelCatalog.selectAvailable')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['manual-alias', 'opaque-future-from-upstream']])
    wrapper.unmount()
  })

  it('allows explicit selection of inventory candidates without reintroducing retired models', async () => {
    getModelCatalog.mockResolvedValue(snapshot([entry('old-retired', 'retired'), entry('active-new'), entry('image-reference', 'active', 'candidate')]))
    const wrapper = mountEditor(['old-retired'])
    await flushPromises()
    expect(wrapper.find('[data-testid="retired-models"]').exists()).toBe(true)
    await click(wrapper, 'modelCatalog.removeRetired')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    await wrapper.setProps({ modelValue: [] })
    await click(wrapper, 'modelCatalog.selectAvailable')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['active-new', 'image-reference']])
    wrapper.unmount()
  })

  it('refreshes the catalog without rewriting a fixed selection and retains it after failure', async () => {
    const wrapper = mountEditor(['fixed-choice'])
    await flushPromises()
    refreshModelCatalog.mockResolvedValueOnce(snapshot([entry('second-future-model')])).mockRejectedValueOnce(new Error('offline'))
    await click(wrapper, 'modelCatalog.refresh')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await click(wrapper, 'modelCatalog.refresh')
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.text()).toContain('second-future-model')
    expect(wrapper.text()).toContain('modelCatalog.loadFailed')
    expect(wrapper.text()).not.toContain('gpt-5.2')
    wrapper.unmount()
  })

  it('enables select-all after a first successful sync without replacing the fixed list', async () => {
    getModelCatalog.mockResolvedValueOnce({ status: 'unavailable', models: [] })
    const wrapper = mountEditor(['fixed-choice'])
    await flushPromises()
    const button = wrapper.findAll('button').find(b => b.text() === 'modelCatalog.selectAvailable')!
    expect(button.attributes('disabled')).toBeDefined()
    refreshModelCatalog.mockResolvedValueOnce(snapshot([entry('new-model')]))
    await click(wrapper, 'modelCatalog.refresh')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await click(wrapper, 'modelCatalog.selectAvailable')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['fixed-choice', 'new-model']])
    wrapper.unmount()
  })

  it('explains an authentication failure while preserving the last successful choices', async () => {
    const wrapper = mountEditor(['fixed-choice'])
    await flushPromises()
    refreshModelCatalog.mockRejectedValueOnce(new Error('authentication_unavailable'))
    await click(wrapper, 'modelCatalog.refresh')
    expect(wrapper.text()).toContain('modelCatalog.syncErrors.authentication_unavailable')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('discards stale account responses when the editor changes account', async () => {
    let resolve!: (value: unknown) => void
    getModelCatalog.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const wrapper = mountEditor()
    await wrapper.setProps({ accountId: 99 })
    await flushPromises()
    resolve(snapshot([entry('other-account-secret-model')]))
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.text()).not.toContain('other-account-secret-model')
    expect(wrapper.text()).toContain('opaque-future-from-upstream')
    wrapper.unmount()
  })

  it('has no built-in fallback when a first catalog read fails', async () => {
    getModelCatalog.mockRejectedValue(new Error('unavailable'))
    const wrapper = mountEditor(['saved-custom'])
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('saved-custom')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('offers image and video inventory in bulk editing without permission badges or automatic selection', async () => {
    getModelCatalog.mockResolvedValue({ status: 'unavailable', models: [
      { ...entry('gpt-image-new', 'active', 'candidate'), kind: 'image' },
      { ...entry('sora-new', 'active', 'candidate'), kind: 'video' }
    ] })
    const wrapper = mount(ModelWhitelistSelector, {
      props: { platform: 'openai', accountIds: [17, 18], modelValue: [] },
      global: { stubs: { ModelIcon: true, Icon: true } }
    })
    await flushPromises()
    expect(getModelCatalog).toHaveBeenCalledWith({ platform: 'openai', view: 'selection' }, expect.any(AbortSignal))
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.text()).toContain('gpt-image-new')
    expect(wrapper.text()).toContain('sora-new')
    expect(wrapper.text()).not.toContain('modelCatalog.pending')
    expect(wrapper.text()).not.toContain('modelCatalog.noVerifiedModels')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await click(wrapper, 'modelCatalog.selectAvailable')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['gpt-image-new', 'sora-new']])
    wrapper.unmount()
  })

  it('deduplicates names only after checking each platform candidate', async () => {
    getModelCatalog.mockImplementation(({ platform }) => Promise.resolve(snapshot([
      { ...entry('shared-name', platform === 'openai' ? 'retired' : 'active'), platform }
    ])))
    const wrapper = mount(ModelWhitelistSelector, {
      props: { platforms: ['openai', 'grok'], modelValue: [] },
      global: { stubs: { ModelIcon: true, Icon: true } }
    })
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(1)
    await click(wrapper, 'modelCatalog.selectAvailable')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['shared-name']])
    wrapper.unmount()
  })
})
