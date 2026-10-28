import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import ModelManagementPage from '../ModelManagementPage.vue'
import type { ModelPolicy } from '@/features/catalog/api/managedModelTypes'

const requests = vi.hoisted(() => ({ policy: vi.fn(), mappings: vi.fn(), sources: vi.fn(), reset: vi.fn(), sync: vi.fn(), save: vi.fn(), saveMappings: vi.fn() }))
vi.mock('@/features/catalog/api/managedModels', () => ({ managedModelsAPI: requests }))
vi.mock('@/features/identity', () => ({ useAuthStore: () => actor }))
const actor = reactive({ isAuthenticated: true, identityScopeKey: '1:session:1:policy:1', permissions: ['models.read', 'models.manage', 'accounts.read', 'billing.cost.read', 'billing.rates.update'],
  can(permission: string): boolean { return this.permissions.includes(permission) }
})
const wrappers: VueWrapper[] = []
const permissions = [...actor.permissions]
const policy: ModelPolicy = { version: 0, platform: 'anthropic', source: 'builtin', catalog_mode: 'policy_only', models: {}, defaults: { enabled: false, max_tokens: 8192, thinking_fallback_budget: 1024, strip_context_1m: false, context_1m_whitelist: [], normalize_thinking: true } }
const CatalogStub = {
  name: 'ManagedCatalogPanel', props: ['canManage', 'canSync', 'policy'],
  emits: ['mapping-draft-state', 'update:policy', 'open-sync', 'reset-policy', 'save-policy'],
  template: `<section data-testid="catalog"><button data-testid="incomplete-mapping" @click="$emit('mapping-draft-state', {dirty: true, valid: false})">Edit</button><button data-testid="reset" @click="$emit('reset-policy')">Reset</button><button data-testid="sync" @click="$emit('open-sync')">Sync</button></section>`
}
const PriceStub = {
  name: 'ManagedPricesPanel', props: ['canManage', 'canRead', 'scope', 'platform'], emits: ['dirty-change', 'busy-change'],
  template: '<section data-testid="prices"><button data-testid="dirty-price" @click="$emit(\'dirty-change\', true)">Edit price</button></section>'
}
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/models', component: ModelManagementPage }, { path: '/elsewhere', component: { template: '<div>Elsewhere</div>' } }] })
  await router.push('/models')
  const wrapper = mount({ template: '<router-view />' }, { global: { plugins: [router, createI18n({ legacy: false, locale: 'en', messages: { en: { ui: { closeModal: 'Close' } } } })], stubs: { ManagedCatalogPanel: CatalogStub, ManagedPricesPanel: PriceStub, BaseDialog: { props: ['show', 'title'], template: '<aside v-if="show"><slot/><slot name="footer"/></aside>' } } } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.resetAllMocks()
  actor.isAuthenticated = true
  actor.identityScopeKey = '1:session:1:policy:1'
  actor.permissions = [...permissions]
  requests.policy.mockImplementation((platform: 'openai' | 'anthropic') => Promise.resolve({ ...policy, platform }))
  requests.mappings.mockResolvedValue({ version: 0, mappings: { 'codex-auto-review': 'example' } })
  requests.sources.mockResolvedValue([{ id: 9, name: 'Authorized source', platform: 'anthropic', type: 'oauth' }])
  vi.spyOn(window, 'confirm').mockReturnValue(false)
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); vi.restoreAllMocks() })

describe('model management composition permissions and draft protection', () => {
  it('does not request private data or render controls when model read permission is absent', async () => {
    actor.permissions = []
    const { wrapper } = await render()
    expect(requests.policy).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('permission')
    expect(wrapper.find('[data-testid="catalog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="prices"]').exists()).toBe(false)
  })
  it('requires accounts.read for source selection even if models.manage is present', async () => {
    actor.permissions = ['models.read', 'models.manage']
    const { wrapper } = await render()
    const panel = wrapper.getComponent(CatalogStub)
    expect(panel.props('canManage')).toBe(true)
    expect(panel.props('canSync')).toBe(false)
    await panel.get('[data-testid="sync"]').trigger('click')
    expect(requests.sources).not.toHaveBeenCalled()
  })
  it('loads price UI only when its tab is requested and billing reads are allowed', async () => {
    const { wrapper } = await render()
    expect(wrapper.find('[data-testid="prices"]').exists()).toBe(false)
    const pricing = wrapper.findAll('nav button')[1]
    await pricing.trigger('click')
    const panel = wrapper.getComponent(PriceStub)
    expect(panel.props('canRead')).toBe(true)
    expect(panel.props('canManage')).toBe(true)
    actor.permissions = ['models.read', 'models.manage']
    await flushPromises()
    expect(wrapper.find('[data-testid="prices"]').exists()).toBe(false)
    expect(wrapper.findAll('nav button')).toHaveLength(1)
  })
  it('keeps pricing read-only without either write permission', async () => {
    actor.permissions = ['models.read', 'billing.cost.read', 'billing.rates.update']
    const { wrapper } = await render()
    await wrapper.findAll('nav button')[1].trigger('click')
    expect(wrapper.getComponent(PriceStub).props('canManage')).toBe(false)
  })
  it('protects incomplete mapping rows when switching platform', async () => {
    const { wrapper } = await render()
    await wrapper.get('[data-testid="incomplete-mapping"]').trigger('click')
    await wrapper.get('select').setValue('openai')
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('Discard'))
    expect(requests.policy).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLSelectElement>('select').element.value).toBe('anthropic')
    vi.mocked(window.confirm).mockReturnValue(true)
    await wrapper.get('select').setValue('openai')
    await flushPromises()
    expect(requests.policy.mock.calls.at(-1)?.[0]).toBe('openai')
  })
  it('protects price drafts before navigation and releases protection after accepted discard', async () => {
    const { wrapper, router } = await render()
    await wrapper.findAll('nav button')[1].trigger('click')
    await wrapper.get('[data-testid="dirty-price"]').trigger('click')
    await router.push('/elsewhere')
    expect(router.currentRoute.value.path).toBe('/models')
    vi.mocked(window.confirm).mockReturnValue(true)
    await router.push('/elsewhere')
    expect(router.currentRoute.value.path).toBe('/elsewhere')
  })
  it('clears source selection and private data after session changes', async () => {
    const { wrapper } = await render()
    await wrapper.get('[data-testid="sync"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Authorized source')
    actor.isAuthenticated = false
    actor.permissions = []
    actor.identityScopeKey = ''
    await flushPromises()
    expect(wrapper.text()).not.toContain('Authorized source')
    expect(wrapper.find('[data-testid="catalog"]').exists()).toBe(false)
  })
  it('requires explicit confirmation before resetting the saved policy', async () => {
    const { wrapper } = await render()
    await wrapper.get('[data-testid="reset"]').trigger('click')
    expect(requests.reset).not.toHaveBeenCalled()
    vi.mocked(window.confirm).mockReturnValue(true)
    requests.reset.mockResolvedValue({ ...policy, version: 1 })
    await wrapper.get('[data-testid="reset"]').trigger('click')
    await flushPromises()
    expect(requests.reset.mock.calls[0].slice(0, 2)).toEqual(['anthropic', 0])
  })

  it('reviews a real conflict response and retains selected edits without automatically saving', async () => {
    const { wrapper } = await render()
    const panel = wrapper.getComponent(CatalogStub)
    panel.vm.$emit('update:policy', { ...policy, catalog_mode: 'worker_only' })
    requests.save.mockRejectedValue({ status: 409, code: 409, reason: 'MODEL_POLICY_CONFLICT' })
    panel.vm.$emit('save-policy')
    await flushPromises()
    expect(wrapper.text()).toContain('server version changed')
    requests.policy.mockResolvedValue({ ...policy, version: 2 })
    await wrapper.get('[data-testid="model-conflict-review"]').findAll('button')[0].trigger('click')
    await flushPromises()
    const checkbox = wrapper.get('input[aria-label="Keep local change: catalog_mode"]')
    await checkbox.setValue(true)
    await wrapper.get('[data-testid="model-conflict-review"]').findAll('button')[1].trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(CatalogStub).props('policy').catalog_mode).toBe('worker_only')
    expect(wrapper.getComponent(CatalogStub).props('policy').version).toBe(2)
    expect(requests.save).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="model-conflict-review"]').exists()).toBe(false)
  })
  it('warns before browser close when an incomplete mapping is edited and removes its listener on disposal', async () => {
    const remove = vi.spyOn(window, 'removeEventListener')
    const { wrapper } = await render()
    await wrapper.get('[data-testid="incomplete-mapping"]').trigger('click')
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('beforeunload', expect.any(Function))
  })
})
