import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import ModelShowcase from '../ModelShowcase.vue'
import { sitePresentationKey, type SitePresentation } from '@/features/site/ports'
import { catalogPresentationKey, catalogScopeKey, useModelPlazaQuery, type CatalogPresentation, type ModelPlazaResponse } from '@/features/catalog'
import { catalogPresentationFixture } from '@/features/catalog/__tests__/presentation'
import { loadApplicationLocale } from '@/app/i18n'

const getModelPlaza = vi.hoisted(() => vi.fn())
vi.mock('@/features/catalog/api/modelPlaza', () => ({ getModelPlaza }))
enableAutoUnmount(afterEach)
afterEach(() => vi.clearAllMocks())
const en = await loadApplicationLocale('en')

async function fixture(overrides: Partial<CatalogPresentation> = {}) {
  const scope = ref('anonymous:console')
  const presentation = ref(catalogPresentationFixture(overrides))
  const refresh = vi.fn(async () => undefined)
  presentation.value.refreshSettings = refresh
  let query!: ReturnType<typeof useModelPlazaQuery>
  const site: SitePresentation = {
    GroupPricingPanel: { render: () => null }, brand: { name: 'sub2api', logo: '', apiBaseUrl: '' }, copy: vi.fn(async () => true),
    createModelQuery() {
      query = useModelPlazaQuery()
      return { data: query.data, loading: query.loading, loadFailed: query.loadFailed, availability: query.availability, load: query.reload }
    }
  }
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }] })
  await router.push('/home')
  const wrapper = mount(ModelShowcase, { global: {
    plugins: [router, createI18n({ legacy: false, locale: 'en', messages: { en } })],
    provide: { [sitePresentationKey as symbol]: site, [catalogPresentationKey as symbol]: presentation, [catalogScopeKey as symbol]: scope }
  } })
  return { wrapper, presentation, scope, refresh, query, router }
}

describe('home live catalog access through the actual catalog query', () => {
  it('keeps the section and reference links when disabled without requesting the protected catalog', async () => {
    const { wrapper, query } = await fixture({ settings: { model_plaza_enabled: false } })
    await flushPromises()
    expect(wrapper.find('[data-testid="model-showcase"]').exists()).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toContain('has not enabled its live model catalog')
    expect(wrapper.findAll('a[target="_blank"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await query.reload()
    expect(getModelPlaza).not.toHaveBeenCalled()
  })

  it('shows sign-in for a restricted anonymous catalog and starts only after authentication', async () => {
    getModelPlaza.mockResolvedValue({ description: 'available', groups: [] })
    const { wrapper, presentation, scope } = await fixture({ settings: { model_plaza_enabled: true, model_plaza_require_auth: true } })
    await flushPromises()
    expect(getModelPlaza).not.toHaveBeenCalled()
    expect(wrapper.get('a[href^="/login"]').attributes('href')).toContain('redirect=/home')
    scope.value = 'user:7:session-a'
    presentation.value.authenticated = true
    await flushPromises()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('No public models')
  })

  it('keeps failed configuration unknown and retries settings before loading any catalog', async () => {
    getModelPlaza.mockResolvedValue({ description: '', groups: [] })
    const { wrapper, presentation, refresh } = await fixture({ settingsLoaded: false, settings: null })
    refresh.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[role="status"] button').trigger('click'); await flushPromises()
    expect(getModelPlaza).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('configuration is not available yet')
    refresh.mockImplementationOnce(async () => {
      presentation.value.settings = { model_plaza_enabled: true }
      presentation.value.settingsLoaded = true
    })
    await wrapper.get('[role="status"] button').trigger('click'); await flushPromises()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('No public models')
  })

  it('invalidates an uncancellable private response immediately when the catalog is disabled', async () => {
    let finish!: (value: ModelPlazaResponse) => void
    getModelPlaza.mockImplementationOnce(() => new Promise<ModelPlazaResponse>(resolve => { finish = resolve }))
    const { wrapper, presentation, query } = await fixture({ authenticated: true })
    const signal = getModelPlaza.mock.calls[0][0].signal as AbortSignal
    presentation.value.settings = { model_plaza_enabled: false }
    expect(signal.aborted).toBe(true)
    finish({ description: 'old private facts', groups: [] })
    await flushPromises()
    expect(query.data.value).toBeNull()
    expect(wrapper.text()).not.toContain('old private facts')
    expect(wrapper.get('[role="status"]').text()).toContain('has not enabled')
  })

  it('rechecks settings after a 404 instead of retrying a newly disabled catalog', async () => {
    getModelPlaza.mockRejectedValueOnce({ status: 404 })
    const { wrapper, presentation, refresh } = await fixture()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    refresh.mockImplementationOnce(async () => { presentation.value.settings = { model_plaza_enabled: false } })
    await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toContain('has not enabled')
  })
})
