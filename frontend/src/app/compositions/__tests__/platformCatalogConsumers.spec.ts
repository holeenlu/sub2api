import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, inject, isReadonly, ref } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import Select from '@/shared/ui/Select.vue'
import ChannelEditorDialog from '@/features/pricing/components/ChannelEditorDialog.vue'
import AdminSubscriptionsPage from '@/features/subscriptions/pages/AdminSubscriptionsPage.vue'
import GroupsPage from '../group-editor/GroupsPage.vue'
import CompositeRoutesDialog from '@/features/access-policy/components/CompositeRoutesDialog.vue'
import AccountTableFilters from '@/features/resources/components/AccountTableFilters.vue'
import ErrorPassthroughRulesModal from '@/features/resources/components/ErrorPassthroughRulesModal.vue'
import OpsDashboardHeader from '@/features/operations/dashboard/components/OpsDashboardHeader.vue'
import { installCatalogServices } from '../catalogServices'
import { installAccessPolicyServices } from '../accessPolicyServices'
import { installResourceServices } from '../resourceServices'
import { installOperationsServices } from '../operationsServices'
import { BUILTIN_PLATFORM_CATALOG, catalogPresentationKey, listPlatformIds, resetPlatformCatalog, setPlatformCatalog } from '@/features/catalog'
import { groupPresentationKey } from '@/features/access-policy/presentation'
import { subscriptionPresentationKey } from '@/features/subscriptions'
import { BILLING_MODE_TOKEN, BILLING_MODE_PER_REQUEST, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS } from '@/features/pricing'
import { groupsAPI } from '@/features/access-policy'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes'
import { adminSubscriptionsAPI } from '@/features/subscriptions'
import channelsAPI from '@/features/pricing/api/channels'
import errorRulesAPI from '@/features/resources/errorPassthrough'
import { pricingCatalogKey } from '@/features/pricing/ports'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { operationsServicesKey } from '@/features/operations/services'
import { apiClient } from '@/shared/http'

const fixture = vi.hoisted(() => ({
  auth: { isAuthenticated: true, identityScopeKey: 'catalog-test:admin', isSuperAdmin: false, isSimpleMode: false,
    adminLandingPath: '/admin/dashboard', can: () => true, canEditAdminField: () => true,
    editableAdminFields: (_schema: string, payload: object) => payload },
  app: { cachedPublicSettings: null, showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn() },
}))
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => fixture.auth }))
vi.mock('@/features/supply', async original => ({ ...await original<typeof import('@/features/supply')>(), useSupplyStore: () => ({ vendor: null }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => fixture.app }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ opsMonitoringEnabled: true, opsRealtimeMonitoringEnabled: false, opsQueryModeDefault: 'auto' }) }))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }) }))
vi.mock('@/app/router', () => ({ default: { currentRoute: { value: { query: {} } }, push: vi.fn().mockResolvedValue(undefined) } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => false, locale: ref('en') }) }))

const cnProviders = ['kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go']
const dynamic = { id: 'coverage_dynamic', display_name: 'Coverage Dynamic', gateway: 'openai' as const, cn_provider: false }
const compositeGroup: AdminGroup = {
  id: 99, name: 'Composite coverage group', description: null, platform: 'composite', rate_multiplier: 1,
  is_exclusive: false, status: 'active', subscription_type: 'standard', daily_limit_usd: null,
  weekly_limit_usd: null, monthly_limit_usd: null, long_context_pricing_enabled: false,
  allow_image_generation: false, allow_batch_image_generation: false, image_rate_independent: false,
  image_rate_multiplier: 1, batch_image_discount_multiplier: 1, batch_image_hold_multiplier: 1,
  image_price_1k: null, image_price_2k: null, image_price_4k: null, video_rate_independent: false,
  video_rate_multiplier: 1, video_price_480p: null, video_price_720p: null, video_price_1080p: null,
  web_search_price_per_call: null, search_price_per_1k: null, audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null, audio_stt_price_per_hour: null, peak_rate_enabled: false,
  peak_start: '', peak_end: '', peak_rate_multiplier: 1, claude_code_only: false,
  fallback_group_id: null, fallback_group_id_on_invalid_request: null, allow_live: false,
  require_oauth_only: false, require_privacy_set: false, created_at: '', updated_at: '',
  force_openai_fast: false, free_openai_fast: false, model_pricing: [], profit_control_enabled: false,
  profit_min_margin: 0, profit_safety_buffer: 0, model_routing: null, model_routing_enabled: false,
  mcp_xml_inject: false, sort_order: 0,
}
const emptyPage = { items: [], total: 0, pages: 0, page: 1, page_size: 20 }
const unexpectedHttp: Array<ReturnType<typeof vi.spyOn>> = []
enableAutoUnmount(afterEach)
beforeEach(() => {
  resetPlatformCatalog()
  vi.clearAllMocks()
  localStorage.clear()
  unexpectedHttp.splice(0)
  for (const method of ['get', 'post', 'put', 'delete'] as const) {
    unexpectedHttp.push(vi.spyOn(apiClient, method).mockRejectedValue(new Error('Unexpected HTTP in platform catalog test')))
  }
  vi.spyOn(groupsAPI, 'list').mockResolvedValue(emptyPage)
  vi.spyOn(groupsAPI, 'getAll').mockResolvedValue([compositeGroup])
  vi.spyOn(groupsAPI, 'getAllIncludingInactive').mockResolvedValue([compositeGroup])
  vi.spyOn(groupsAPI, 'listCompositeRoutes').mockResolvedValue([])
  vi.spyOn(groupsAPI, 'getUsageSummary').mockResolvedValue([])
  vi.spyOn(groupsAPI, 'getCapacitySummary').mockResolvedValue([])
  vi.spyOn(adminSubscriptionsAPI, 'list').mockResolvedValue(emptyPage)
  vi.spyOn(channelsAPI, 'list').mockResolvedValue(emptyPage)
  vi.spyOn(errorRulesAPI, 'list').mockResolvedValue([])
})
afterEach(() => {
  for (const request of unexpectedHttp) expect(request).not.toHaveBeenCalled()
  resetPlatformCatalog()
  vi.restoreAllMocks()
  document.body.style.overflow = ''
})

// These are the production app installers. Only identity state and remote reads are fixtures.
function appBindings() {
  const app = createApp({})
  installCatalogServices(app)
  installResourceServices(app)
  installOperationsServices(app, async () => true)
  installAccessPolicyServices(app)
  return { app, provide: app._context.provides }
}
function surface() {
  return { provide: appBindings().provide, renderStubDefaultSlot: true,
    stubs: { Select: false, BaseDialog: false, TablePageLayout: false, Teleport: true, Transition: true, RouterLink: true } }
}
function options(wrapper: VueWrapper) {
  const select = wrapper.findAllComponents(Select).find(control =>
    (control.props('options') as Array<{ value: unknown }>).some(option => option.value === 'anthropic'))
  expect(select, 'the real platform Select must be rendered').toBeDefined()
  return select!
}
function values(control: VueWrapper) {
  return (control.props('options') as Array<{ value: unknown }>).map(option => option.value).filter(value => value !== '' && value !== null)
}
async function registerDynamic() {
  setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, dynamic] })
  await flushPromises()
}
function assertDirectory(actual: unknown[], composite: boolean) {
  expect(actual).toEqual(composite ? [...listPlatformIds(), 'composite'] : listPlatformIds())
  expect(actual).toEqual(expect.arrayContaining(cnProviders))
  expect(actual.includes('composite')).toBe(composite)
}

async function exerciseSelect(wrapper: VueWrapper, composite: boolean) {
  await flushPromises()
  const control = options(wrapper)
  assertDirectory(values(control), composite)
  await registerDynamic()
  assertDirectory(values(control), composite)
  expect(values(control)).toContain(dynamic.id)
  await control.get('.select-trigger').trigger('click')
  const option = control.findAll('[role="option"]').find(item => item.text().includes(dynamic.display_name))
  expect(option, 'registered platform is visible in the real dropdown').toBeDefined()
  await option!.trigger('click')
  await flushPromises()
  expect(control.emitted('update:modelValue')?.at(-1)).toEqual([dynamic.id])
}

describe('one live platform registry across real app consumers', () => {
  it('app projects the existing pricing taxonomy and exact reasoning effort order', () => {
    const { app } = appBindings()
    const catalog = app.runWithContext(() => inject(catalogPresentationKey))!
    expect(catalog.value.pricing).toEqual({
      token: BILLING_MODE_TOKEN,
      perRequest: BILLING_MODE_PER_REQUEST,
      image: BILLING_MODE_IMAGE,
      video: BILLING_MODE_VIDEO,
      reasoningEffortLevels: REASONING_EFFORT_LEVELS
    })
    expect(catalog.value.pricing.reasoningEffortLevels).toBe(REASONING_EFFORT_LEVELS)
    expect(catalog.value.pricing.reasoningEffortLevels).toEqual(['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'])
  })

  it('app pricing, resources and operations providers project the same live concrete registry', async () => {
    const { app } = appBindings()
    const pricing = app.runWithContext(() => inject(pricingCatalogKey))!
    const resources = app.runWithContext(() => inject(resourceEditorServicesKey))!
    const operations = app.runWithContext(() => inject(operationsServicesKey))!
    const group = app.runWithContext(() => inject(groupPresentationKey))!
    const subscriptions = app.runWithContext(() => inject(subscriptionPresentationKey))!
    const check = () => {
      assertDirectory(pricing.listPlatformIds(), false)
      assertDirectory(resources.catalog.listPlatforms().map(item => item.id), false)
      assertDirectory(operations.platformOptions.value.map(item => item.value), false)
      assertDirectory(group.concretePlatformOptions.value.map(item => item.value), false)
      assertDirectory(subscriptions.groupPlatformOptions.value.map(item => item.value), true)
      expect(isReadonly(group.concretePlatformOptions.value)).toBe(true)
      expect(isReadonly(subscriptions.groupPlatformOptions.value)).toBe(true)
    }
    check()
    await registerDynamic()
    check()
    expect(operations.platformOptions.value.find(item => item.value === dynamic.id)?.label).toBe(dynamic.display_name)
    expect(group.concretePlatformOptions.value.find(item => item.value === dynamic.id)?.label).toBe(dynamic.display_name)
    expect(subscriptions.groupPlatformOptions.value.find(item => item.value === dynamic.id)?.label).toBe(dynamic.display_name)
  })

  it('subscription group filters include composite and newly registered platforms', async () => {
    const wrapper = shallowMount(AdminSubscriptionsPage, { global: surface() })
    await exerciseSelect(wrapper, true)
  })

  it('group list filters include composite and newly registered platforms', async () => {
    const wrapper = shallowMount(GroupsPage, { global: surface() })
    await exerciseSelect(wrapper, true)
  })

  it('composite route targets include every concrete provider but never composite itself', async () => {
    const wrapper = shallowMount(CompositeRoutesDialog, { props: { group: compositeGroup }, global: surface() })
    await exerciseSelect(wrapper, false)
  })

  it('account platform filters consume app catalog options and emit the new platform', async () => {
    const wrapper = shallowMount(AccountTableFilters, { props: { searchQuery: '', filters: { platform: '', type: '', status: '', privacy_mode: '', group: '' }, groups: [] }, global: surface() })
    await exerciseSelect(wrapper, false)
    expect(wrapper.emitted('update:filters')?.at(-1)).toEqual([expect.objectContaining({ platform: dynamic.id })])
  })

  it('error passthrough platform checkboxes use the same concrete provider directory', async () => {
    const wrapper = shallowMount(ErrorPassthroughRulesModal, { props: { show: false }, global: surface() })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.errorPassthrough.createRule')!.trigger('click')
    const selected = () => wrapper.findAll('input[type="checkbox"]').map(input => input.attributes('value')).filter(Boolean)
    assertDirectory(selected(), false)
    await registerDynamic()
    assertDirectory(selected(), false)
    const checkbox = wrapper.get('input[value="' + dynamic.id + '"]')
    await checkbox.setValue(true)
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
  })

  it('operations request platform filters receive the app concrete-platform projection', async () => {
    const wrapper = shallowMount(OpsDashboardHeader, { props: { platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null }, global: surface() })
    await exerciseSelect(wrapper, false)
    expect(wrapper.emitted('update:platform')?.at(-1)).toEqual([dynamic.id])
  })

  it('channel pricing and model mapping accept composite groups for all CN and dynamic concrete providers', async () => {
    const wrapper = shallowMount(ChannelEditorDialog, { props: { channel: null }, global: surface() })
    await flushPromises()
    const platformLabel = (id: string) => wrapper.findAll('label').find(label => label.text() === 'admin.groups.platforms.' + id)
    const displayedIds = () => wrapper.findAll('label').map(label => label.text()).filter(text => text.startsWith('admin.groups.platforms.')).map(text => text.slice('admin.groups.platforms.'.length))
    assertDirectory(displayedIds(), false)
    await registerDynamic()
    assertDirectory(displayedIds(), false)
    for (const id of [...cnProviders, dynamic.id]) {
      expect(platformLabel(id)).toBeDefined()
      await platformLabel(id)!.get('input[type="checkbox"]').setValue(true)
      await wrapper.findAll('button.channel-tab').find(button => button.text() === 'admin.groups.platforms.' + id)!.trigger('click')
      const groupLabel = wrapper.findAll('label').find(label => label.isVisible() && label.text().includes(compositeGroup.name))
      expect(groupLabel, 'composite group remains eligible for ' + id).toBeDefined()
      await groupLabel!.get('input[type="checkbox"]').setValue(true)
      expect((groupLabel!.get('input').element as HTMLInputElement).checked).toBe(true)
    }
  })
})
