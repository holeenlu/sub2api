import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
const mocks = vi.hoisted(() => ({ catalog: vi.fn(), save: vi.fn(), error: vi.fn() }))
vi.mock('@/api/admin/models', () => ({ modelsAPI: { catalog: mocks.catalog, saveAccountModels: mocks.save } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const row = (id: string, enabled = true) => ({ id, display_name: id, enabled })
function mountSelector(props = {}) { return mount(ModelWhitelistSelector, { props: { modelValue: [] as string[], platform: 'openai', ...props }, global: { stubs: { ModelIcon: true, Icon: true } } }) }
async function open(wrapper: ReturnType<typeof mountSelector>) { await flushPromises(); await wrapper.get('div.cursor-pointer').trigger('click') }
const option = (wrapper: ReturnType<typeof mountSelector>, id: string) => wrapper.findAll('[data-testid="select-model"]').find(button => button.text().includes(id))!

beforeEach(() => { vi.clearAllMocks(); mocks.catalog.mockResolvedValue([row('model-a'), row('model-b'), row('model-off', false)]); mocks.save.mockResolvedValue({ models: ['model-b'] }) })
describe('unified account model references', () => {
  it('saves against the hydrated account state instead of the empty mount snapshot', async () => {
    const wrapper = mountSelector({ accountId: 7 })
    await wrapper.setProps({ modelValue: ['model-a', 'model-b'] }); await open(wrapper)
    await option(wrapper, 'model-a').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(7, ['model-a', 'model-b'], ['model-b'])
  })
  it('renders the complete catalog, locks disabled new grants and retains removable history', async () => {
    const wrapper = mountSelector({ modelValue: ['historical-model'] }); await open(wrapper)
    expect(wrapper.text()).toContain('model-off'); expect(option(wrapper, 'model-off').attributes('disabled')).toBeDefined()
    expect(option(wrapper, 'historical-model').attributes('disabled')).toBeUndefined()
    await option(wrapper, 'historical-model').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    expect(mocks.catalog).toHaveBeenCalledTimes(1)
  })
  it('uses inherited models as the checkbox state and emits server-normalized inheritance', async () => {
    mocks.save.mockResolvedValue({ models: null })
    const wrapper = mountSelector({ modelValue: ['model-a'], accountId: 7 }); await open(wrapper)
    await option(wrapper, 'model-b').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(7, ['model-a'], ['model-a', 'model-b'])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })
  it('serializes checkbox saves and rolls back failures to the saved state', async () => {
    let reject!: (error: Error) => void
    mocks.save.mockReturnValue(new Promise((_, fail) => { reject = fail }))
    const wrapper = mountSelector({ modelValue: ['model-a', 'model-b'], accountId: 7 }); await open(wrapper)
    await option(wrapper, 'model-a').trigger('click'); await option(wrapper, 'model-b').trigger('click')
    expect(mocks.save).toHaveBeenCalledTimes(1)
    reject(new Error('conflict')); await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['model-a', 'model-b']])
    expect(mocks.error).toHaveBeenCalledWith('conflict')
  })
  it('ignores a stale catalog response after switching platforms', async () => {
    let finish!: (rows: unknown[]) => void
    mocks.catalog.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mountSelector(); await wrapper.setProps({ platform: 'anthropic' }); await flushPromises()
    finish([row('stale-openai-model')]); await open(wrapper)
    expect(wrapper.text()).not.toContain('stale-openai-model')
  })
})
