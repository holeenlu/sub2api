import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import type { App, Plugin } from 'vue'
import { ModelIcon } from '../../modelIcon'
import type { ModelPlazaGroup } from '../../api/types'
import { installSiteServices } from '@/app/compositions/siteServices'
import DocsHomeView from '@/features/site/docs/pages/DocsHomeView.vue'
import DocsModelsView from '@/features/site/docs/pages/DocsModelsView.vue'
import DocsModelView from '@/features/site/docs/pages/DocsModelView.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import { resourceEditorServicesKey, type ResourceManagedModel } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import ProviderIcon from '@/features/operations/monitor/user/ProviderIcon.vue'
import { uiFeedbackKey } from '@/shared/ui/feedback'

enableAutoUnmount(afterEach)

vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({ isAuthenticated: false, adminLandingPath: '/admin/dashboard', user: null })
}))
vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({ siteName: 'Test site', siteLogo: '', apiBaseUrl: 'https://api.example.test', cachedPublicSettings: null, publicSettingsLoaded: true })
}))

// Run the production installer: the actual docs consumers receive the catalog
// component through their site port, without mocking ModelIcon or its resolver.
const siteServices: Plugin = {
  install(app: App) { installSiteServices(app, async () => true) }
}
const docsGlobal = {
  plugins: [siteServices],
  stubs: { RouterLink: RouterLinkStub, DocsGroupSelect: true, DocsPricingTable: true, DocsCodeTabs: true }
}

function modelGroup(names: string[]): ModelPlazaGroup {
  return {
    id: 12, name: 'Test directory', description: '', platform: 'openai', subscription_type: 'standard',
    rate_multiplier: 1, peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    is_exclusive: false, image_rate_independent: false, image_rate_multiplier: 1,
    video_rate_independent: false, video_rate_multiplier: 1, long_context_pricing_enabled: false,
    models: names.map(name => ({ name, platform: 'openai', pricing: null, official_pricing: null }))
  }
}

describe('model icon through actual domain consumers', () => {
  it('renders the docs home featured models through the installed site presentation', () => {
    const wrapper = mount(DocsHomeView, { props: { groups: [], selectedGroupId: null }, global: docsGlobal })
    const icons = wrapper.findAllComponents(ModelIcon)
    expect(icons.map(icon => icon.props('model'))).toEqual(['gpt-6-astra', 'gpt-5.6-sol', 'claude-opus-5', 'gpt-image-2.5-flare'])
    expect(icons.map(icon => icon.get('svg').attributes('width'))).toEqual(['24px', '24px', '24px', '24px'])
    expect(icons.map(icon => icon.get('path').attributes('fill'))).toEqual(['#000000', '#000000', '#D97706', '#000000'])
    expect(wrapper.findAllComponents(RouterLinkStub).map(link => link.props('to'))).toContain('/docs/models/gpt-6-astra')
  })

  it('keeps server-listed unknown models visible and does not infer admission from an icon', async () => {
    const group = modelGroup(['gpt-4o', 'claude-opus-4', 'unlisted/custom'])
    const wrapper = mount(DocsModelsView, {
      props: { groups: [group], selectedGroup: group, selectedGroupId: group.id, loading: false, loadFailed: false },
      global: docsGlobal
    })
    expect(wrapper.findAllComponents(ModelIcon).map(icon => icon.props('model'))).toEqual(group.models.map(model => model.name))
    expect(wrapper.findAll('svg.model-icon')).toHaveLength(2)
    expect(wrapper.get('.model-icon-fallback').text()).toBe('U')
    expect(wrapper.get('.model-icon-fallback').attributes('style')).toContain('width: 24px')
    await wrapper.setProps({ selectedGroup: null, loadFailed: true })
    expect(wrapper.findAllComponents(ModelIcon)).toHaveLength(0)
    expect(wrapper.text()).toContain('docs.catalogUnavailable')
  })

  it('preserves model detail size, official external link protections and unknown availability', async () => {
    const wrapper = mount(DocsModelView, {
      props: { modelId: 'gpt-6-astra', selectedGroup: null, apiBaseUrl: 'https://api.example.test' }, global: docsGlobal
    })
    expect(wrapper.getComponent(ModelIcon).get('svg').attributes('width')).toBe('28px')
    expect(wrapper.get('a[target="_blank"]').attributes()).toMatchObject({
      href: 'https://developers.openai.com/api/docs/models/gpt-6-astra', target: '_blank', rel: 'noopener noreferrer'
    })
    expect(wrapper.text()).toContain('docs.unknownAvailability')
    await wrapper.setProps({ modelId: 'unknown/custom', selectedGroup: modelGroup(['unknown/custom']) })
    expect(wrapper.getComponent(ModelIcon).get('.model-icon-fallback').text()).toBe('U')
    expect(wrapper.find('a[target="_blank"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('docs.available')
  })

  it('keeps whitelist chips, dropdown artwork and selection events through resource services', async () => {
    const services = editorTestServices()
    const catalog: ResourceManagedModel[] = ['gpt-4o', 'claude-opus-4', 'unlisted/custom'].map(id => ({ id, display_name: id, enabled: true }))
    services.managedModelCatalog = vi.fn().mockResolvedValue(catalog)
    const wrapper = mount(ModelWhitelistSelector, {
      props: { modelValue: ['gpt-4o', 'unlisted/custom'], platform: 'openai' },
      global: { provide: {
        [resourceEditorServicesKey as symbol]: { ...services, components: { ...services.components, ModelIcon } },
        [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() }
      } }
    })
    await flushPromises()
    expect(wrapper.findAllComponents(ModelIcon).map(icon => icon.props('size'))).toEqual(['14px', '14px'])
    expect(wrapper.get('.model-icon-fallback').text()).toBe('U')
    await wrapper.get('div.cursor-pointer').trigger('click')
    const rows = wrapper.findAll('[data-testid="model-option"]')
    expect(rows.map(row => row.getComponent(ModelIcon).props('model'))).toEqual(['gpt-4o', 'claude-opus-4', 'unlisted/custom'])
    expect(rows.map(row => row.getComponent(ModelIcon).props('size'))).toEqual(['18px', '18px', '18px'])
    const claude = rows.find(row => row.getComponent(ModelIcon).props('model') === 'claude-opus-4')
    if (!claude) throw new Error('Expected server-listed Claude model')
    expect(claude.get('path[fill]').attributes('fill')).toBe('#D97706')
    await claude.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-4o', 'unlisted/custom', 'claude-opus-4']]])
  })

  it('keeps the separate operations provider icon monochrome with its numeric size and empty fallback', async () => {
    const wrapper = mount(ProviderIcon, { props: { provider: 'openai', size: 22 } })
    expect(wrapper.get('svg').attributes()).toMatchObject({ width: '22', height: '22', fill: 'currentColor', 'aria-hidden': 'true' })
    expect(wrapper.get('path').attributes('fill')).toBeUndefined()
    const model = mount(ModelIcon, { props: { model: 'gpt-4o' } })
    expect(wrapper.get('path').attributes('d')).toBe(model.get('path').attributes('d'))
    await wrapper.setProps({ provider: '' })
    expect(wrapper.get('span').text()).toBe('?')
    expect(wrapper.get('span').attributes('style')).toContain('font-size: 11px')
    expect(wrapper.classes()).toContain('text-gray-500')
  })
})
