import { describe, expect, it, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import DocsPricingTable from '../DocsPricingTable.vue'
import DocsModelsView from '@/views/docs/DocsModelsView.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const group = { id: 1, models: [{ name: 'public/custom', platform: 'openai', pricing: null, official_pricing: null }] } as ModelPlazaGroup

describe('documentation uses the group catalog', () => {
  it('renders a custom model through the same group quote component', () => {
    const wrapper = shallowMount(DocsPricingTable, { props: { kind: 'chat', selectedGroup: group, modelIds: ['public/custom'] } })
    expect(wrapper.findComponent({ name: 'PlazaGroupSection' }).props('group').models).toEqual(group.models)
    wrapper.unmount()
  })
  it('does not fill an unavailable directory with static models or prices', () => {
    const prices = shallowMount(DocsPricingTable, { props: { kind: 'chat', selectedGroup: null } })
    expect(prices.findComponent({ name: 'PlazaGroupSection' }).exists()).toBe(false)
    const models = shallowMount(DocsModelsView, { props: { groups: [], selectedGroup: null, selectedGroupId: null, loading: false, loadFailed: true }, global: { stubs: { RouterLink: true } } })
    expect(models.findAllComponents({ name: 'RouterLink' })).toHaveLength(0)
    expect(models.text()).toContain('docs.catalogUnavailable')
    prices.unmount(); models.unmount()
  })
})
