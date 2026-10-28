import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { catalogPresentationFixture } from './presentation'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AvailableChannelsPage from '../pages/AvailableChannelsPage.vue'
import { catalogNotificationsKey, catalogPresentationKey, catalogScopeKey, catalogUserGroupRatesKey } from '../ports'
import type { CatalogPresentation } from '../ports'
import type { UserAvailableChannel } from '../api/types'
const getAvailableChannels = vi.hoisted(() => vi.fn())
vi.mock('../api/availableChannels', () => ({ getAvailableChannels }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => getAvailableChannels.mockReset())
const source: UserAvailableChannel[] = [{ name: 'Primary', description: '', platforms: [
  { platform: 'openai', groups: [], supported_models: [{ name: 'gpt-example', platform: 'openai', pricing: null }] },
  { platform: 'unknown', groups: [], supported_models: [] }
] }]
function setup(rates = vi.fn().mockResolvedValue({ 1: 0 })) {
  const scope = ref('session1:user:1')
  const showError = vi.fn()
  const presentation = ref<CatalogPresentation>(catalogPresentationFixture({ authenticated: true, settings: { server_utc_offset: '+08:00' } }))
  const wrapper = mount(AvailableChannelsPage, { global: {
    provide: {
      [catalogScopeKey as symbol]: scope,
      [catalogPresentationKey as symbol]: presentation,
      [catalogNotificationsKey as symbol]: { showError },
      [catalogUserGroupRatesKey as symbol]: rates
    },
    stubs: {
      TablePageLayout: { template: '<main><slot name="filters" /><slot name="table" /></main>' },
      Icon: true,
      AvailableChannelsTable: { name: 'AvailableChannelsTable', props: ['rows', 'loading', 'userGroupRates', 'serverUtcOffset', 'emptyLabel'], template: '<section>{{ rows.length ? rows[0].name : emptyLabel }}</section>' }
    }
  } })
  return { wrapper, scope, presentation, showError }
}

describe('AvailableChannelsPage app-port integration', () => {
  it('renders without global stores and forwards pricing rates and reactive timezone', async () => {
    getAvailableChannels.mockResolvedValue(source)
    const { wrapper, presentation } = setup()
    await flushPromises()
    const table = wrapper.getComponent({ name: 'AvailableChannelsTable' })
    expect(table.props('rows')).toEqual(source)
    expect(table.props('userGroupRates')).toEqual({ 1: 0 })
    expect(table.props('serverUtcOffset')).toBe('+08:00')
    presentation.value = { ...presentation.value, settings: null }
    await wrapper.get('input').setValue('gpt-example')
    expect(table.props('rows')[0].platforms).toHaveLength(1)
    expect(table.props('serverUtcOffset')).toBeUndefined()
    await wrapper.get('input').setValue('missing')
    expect(wrapper.text()).toContain('availableChannels.empty')
  })
  it('keeps rates failure silent, reports a primary refresh error, and retains the current snapshot', async () => {
    getAvailableChannels.mockResolvedValueOnce(source).mockRejectedValueOnce(new Error('channels unavailable'))
    const { wrapper, showError } = setup(vi.fn().mockRejectedValue(new Error('rates unavailable')))
    await flushPromises()
    expect(wrapper.text()).toContain('Primary')
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.getComponent({ name: 'AvailableChannelsTable' }).props('userGroupRates')).toEqual({})
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('Primary')
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  })
  it('clears the search on scope change and suppresses a previous-scope error notification', async () => {
    let rejectOld!: (error: Error) => void
    getAvailableChannels.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectOld = reject })).mockResolvedValueOnce([])
    const { wrapper, scope, showError } = setup()
    await wrapper.get('input').setValue('private search')
    scope.value = 'session2:user:2'
    rejectOld(new Error('stale'))
    await flushPromises()
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.getComponent({ name: 'AvailableChannelsTable' }).props('rows')).toEqual([])
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('availableChannels.empty')
  })
})
