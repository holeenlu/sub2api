import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelsView from '../ModelsView.vue'

const mocks = vi.hoisted(() => ({ policy: vi.fn(), sources: vi.fn(), sync: vi.fn(), save: vi.fn(), error: vi.fn() }))
vi.mock('@/api/admin/models', () => ({ modelsAPI: mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: mocks.error }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ can: (permission: string) => permission !== 'billing.cost.read' }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
vi.mock('vue-router', async () => ({ ...await vi.importActual<typeof import('vue-router')>('vue-router'), onBeforeRouteLeave: vi.fn() }))

const model = (id: string, enabled = true) => ({ id, display_name: id, kind: 'chat', enabled, sort: 1, aliases: [], params: {}, betas: {} })
const policy = (version: number, models: Record<string, ReturnType<typeof model>>) => ({
  version, platform: 'anthropic', source: 'settings', catalog_mode: 'policy_only', models,
  defaults: { enabled: true, max_tokens: 0, thinking_fallback_budget: 0, strip_context_1m: false, context_1m_whitelist: [], normalize_thinking: false },
})
function mountView() {
  return mount(ModelsView, { global: { stubs: {
    AppLayout: defineComponent({ template: '<main><slot /></main>' }),
    BaseDialog: defineComponent({ props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' }),
    ModelPriceDialog: true,
  } } })
}
const button = (wrapper: ReturnType<typeof mountView>, label: string) => wrapper.findAll('button').find(item => item.text() === label)!

beforeEach(() => {
  vi.clearAllMocks()
  mocks.policy.mockResolvedValue(policy(1, { a: model('a'), b: model('b', false) }))
  mocks.sources.mockResolvedValue([{ id: 7, name: 'source', type: 'oauth' }])
  mocks.sync.mockResolvedValue(policy(2, { a: model('a'), b: model('b', false), c: model('c', false) }))
})

describe('model catalog synchronization', () => {
  it('keeps unsaved edits and the saved disabled state while adopting the new version', async () => {
    const wrapper = mountView()
    await flushPromises()
    const first = wrapper.findAll('article')[0]
    await first.get('input[type="checkbox"]').setValue(false)
    await button(wrapper, 'modelManagement.syncCatalog').trigger('click')
    await flushPromises()
    await wrapper.find('section input[type="checkbox"][value="7"]').setValue(true)
    await wrapper.findAll('button').filter(item => item.text() === 'modelManagement.syncCatalog').at(-1)!.trigger('click')
    await flushPromises()
    const articles = wrapper.findAll('article')
    expect((articles[0].get('input').element as HTMLInputElement).checked).toBe(false)
    expect((articles[1].get('input').element as HTMLInputElement).checked).toBe(false)
    expect(articles).toHaveLength(3)
    expect(mocks.sync).toHaveBeenCalledWith('anthropic', 1, [7])
    mocks.save.mockImplementation(async value => value)
    await button(wrapper, 'modelManagement.saveChanges').trigger('click')
    await flushPromises()
    expect(mocks.save.mock.calls[0][0]).toMatchObject({ version: 2, models: { a: { enabled: false }, b: { enabled: false }, c: { enabled: false } } })
  })

  it('retains the draft when synchronization fails', async () => {
    mocks.sync.mockRejectedValue(new Error('sync failed'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('article')[0].get('input').setValue(false)
    await button(wrapper, 'modelManagement.syncCatalog').trigger('click')
    await flushPromises()
    await wrapper.find('section input[type="checkbox"][value="7"]').setValue(true)
    await wrapper.findAll('button').filter(item => item.text() === 'modelManagement.syncCatalog').at(-1)!.trigger('click')
    await flushPromises()
    expect((wrapper.findAll('article')[0].get('input').element as HTMLInputElement).checked).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith('sync failed')
  })

  it('preserves changes made while synchronization is pending', async () => {
    let finish!: (value: ReturnType<typeof policy>) => void
    mocks.sync.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await button(wrapper, 'modelManagement.syncCatalog').trigger('click')
    await flushPromises()
    await wrapper.find('section input[type="checkbox"][value="7"]').setValue(true)
    await wrapper.findAll('button').filter(item => item.text() === 'modelManagement.syncCatalog').at(-1)!.trigger('click')
    await wrapper.find('details input[type="checkbox"]').setValue(false)
    finish(policy(2, { a: model('a'), b: model('b', false) }))
    await flushPromises()
    expect((wrapper.find('details input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
    mocks.save.mockImplementation(async value => value)
    await button(wrapper, 'modelManagement.saveChanges').trigger('click')
    await flushPromises()
    expect(mocks.save.mock.calls[0][0]).toMatchObject({ version: 2, defaults: { enabled: false } })
  })
})
