import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, inject, reactive, ref, type InjectionKey } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { installPaymentsServices } from '../paymentsServices'
import { installCatalogServices } from '../catalogServices'
import { installAccessPolicyServices } from '../accessPolicyServices'
import { peakRatePresentation } from '@/features/pricing'
import { paymentsServicesKey, paymentAPI } from '@/features/payments'
import { catalogPresentationKey } from '@/features/catalog'
import { subscriptionPresentationKey } from '@/features/subscriptions'
import { groupPresentationKey, GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import CheckoutPage from '@/features/payments/pages/CheckoutPage.vue'
import SubscriptionPlanCard from '@/features/payments/components/SubscriptionPlanCard.vue'
import UserSubscriptionsPage from '@/features/subscriptions/pages/UserSubscriptionsPage.vue'
import PlazaGroupSection from '@/features/catalog/components/PlazaGroupSection.vue'
import AvailableChannelsTable from '@/features/catalog/components/AvailableChannelsTable.vue'
import subscriptionsAPI from '@/features/subscriptions/api/user'
import type { Group } from '@/features/access-policy/api/groupTypes'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes'
import type { UserSubscription } from '@/features/subscriptions/api/types'
import type { ModelPlazaGroup, UserAvailableChannel } from '@/features/catalog/api/types'
import { apiClient } from '@/shared/http'

const route = { path: '/purchase', query: {} as Record<string, string> }
const settings = reactive({
  cachedPublicSettings: { server_utc_offset: '+05:30' as string | undefined, subscription_enabled: true },
  publicSettingsLoaded: true, showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn()
})
const subscriptions = reactive({ activeSubscriptions: [] as UserSubscription[], fetchActiveSubscriptions: async () => undefined })
vi.mock('@/app/model/app', () => ({ useAppStore: () => settings }))
vi.mock('@/features/identity', async original => ({
  ...await original<typeof import('@/features/identity')>(),
  useAuthStore: () => ({
    isAuthenticated: true, identityScopeKey: 'peak-test:user', isSuperAdmin: false, isSimpleMode: false,
    user: { id: 9, username: 'user', balance: 0 }, can: () => true,
    canEditAdminField: () => true, editableAdminFields: (_schema: string, payload: object) => payload,
    refreshUser: vi.fn().mockResolvedValue(undefined)
  })
}))
vi.mock('@/features/subscriptions', async original => ({
  ...await original<typeof import('@/features/subscriptions')>(), useSubscriptionStore: () => subscriptions
}))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => ({ isCurrentStep: () => false }) }))
vi.mock('@/app/router', () => ({ default: { currentRoute: { value: { query: {} } }, push: vi.fn().mockResolvedValue(undefined) } }))
vi.mock('vue-router', async original => ({
  ...await original<typeof import('vue-router')>(),
  useRoute: () => route,
  useRouter: () => ({ replace: vi.fn().mockResolvedValue(undefined), push: vi.fn().mockResolvedValue(undefined) })
}))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values?.window ?? key, locale: ref('en') })
}))

function groupFixture(): Group {
  return {
    id: 3, name: 'Peak group', description: '', platform: 'openai', rate_multiplier: 1,
    is_exclusive: false, status: 'active', subscription_type: 'subscription', daily_limit_usd: null,
    weekly_limit_usd: null, monthly_limit_usd: null, long_context_pricing_enabled: true,
    allow_image_generation: false, allow_batch_image_generation: false, image_rate_independent: false,
    image_rate_multiplier: 1, batch_image_discount_multiplier: 1, batch_image_hold_multiplier: 1,
    image_price_1k: null, image_price_2k: null, image_price_4k: null, video_rate_independent: false,
    video_rate_multiplier: 1, video_price_480p: null, video_price_720p: null, video_price_1080p: null,
    web_search_price_per_call: null, search_price_per_1k: null, audio_realtime_price_per_min: null,
    audio_tts_price_per_million_chars: null, audio_stt_price_per_hour: null,
    peak_rate_enabled: true, peak_start: '22:00', peak_end: '06:00', peak_rate_multiplier: 0,
    claude_code_only: false, fallback_group_id: null, fallback_group_id_on_invalid_request: null,
    allow_live: false, require_oauth_only: false, require_privacy_set: false, created_at: '', updated_at: ''
  }
}

function appBindings() {
  const app = createApp({})
  installPaymentsServices(app)
  installCatalogServices(app)
  installAccessPolicyServices(app)
  function get<T>(key: InjectionKey<T>): T {
    const value = app.runWithContext(() => inject(key))
    if (value === undefined) throw new Error('Missing app binding: ' + key.description)
    return value
  }
  return { get, provide: app._context.provides }
}

const consumers = ['GroupBadge', 'GroupOptionItem', 'SubscriptionPlanCard', 'CheckoutPage plan',
  'CheckoutPage subscription', 'UserSubscriptionsPage', 'PlazaGroupSection', 'AvailableChannelsTable'] as const
type Consumer = typeof consumers[number]

async function mountConsumer(consumer: Consumer) {
  const bindings = appBindings()
  const group = reactive(groupFixture())
  const plan = reactive<SubscriptionPlan>({
    id: 7, group_id: 3, group_platform: 'openai', name: 'Peak plan', description: '', price: 10,
    validity_days: 30, validity_unit: 'day', features: [], for_sale: true, sort_order: 1,
    peak_rate_enabled: true, peak_start: '22:00', peak_end: '06:00', peak_rate_multiplier: 0
  })
  const subscription: UserSubscription = {
    id: 1, user_id: 9, group_id: 3, status: 'active', starts_at: '', expires_at: null,
    daily_usage_usd: 0, weekly_usage_usd: 0, monthly_usage_usd: 0, daily_window_start: null,
    weekly_window_start: null, monthly_window_start: null, created_at: '', updated_at: '', group
  }
  subscriptions.activeSubscriptions = [subscription]
  vi.spyOn(subscriptionsAPI, 'getMySubscriptions').mockResolvedValue([subscription])
  vi.spyOn(paymentAPI, 'getCheckoutInfo').mockResolvedValue({ data: {
    methods: {}, global_min: 0, global_max: 0, plans: [plan], balance_disabled: false,
    balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0,
    help_text: '', help_image_url: '', stripe_publishable_key: '', recharge_bonus_tiers: [],
    recharge_bonus_mode: 'bonus', recharge_bonus_notice: ''
  } })
  const global = { provide: bindings.provide, stubs: { Teleport: true, RouterLink: true } }
  const badgeProps = () => ({ name: group.name, platform: group.platform, peakRateEnabled: group.peak_rate_enabled,
    peakStart: group.peak_start, peakEnd: group.peak_end, peakRateMultiplier: group.peak_rate_multiplier })
  let wrapper: VueWrapper
  switch (consumer) {
    case 'GroupBadge': wrapper = shallowMount(GroupBadge, { props: badgeProps(), global }); break
    case 'GroupOptionItem': wrapper = shallowMount(GroupOptionItem, { props: badgeProps(), global }); break
    case 'SubscriptionPlanCard': wrapper = shallowMount(SubscriptionPlanCard, { props: { plan }, global }); break
    case 'CheckoutPage plan':
    case 'CheckoutPage subscription':
      route.query = consumer === 'CheckoutPage plan' ? { tab: 'subscription', group: '3' } : { tab: 'subscription' }
      wrapper = shallowMount(CheckoutPage, { global })
      break
    case 'UserSubscriptionsPage': wrapper = shallowMount(UserSubscriptionsPage, { global }); break
    case 'PlazaGroupSection': {
      const plazaGroup = reactive<ModelPlazaGroup>({ ...group, description: '', models: [] })
      // The child badge is stubbed so only this consumer's note can satisfy assertions.
      wrapper = shallowMount(PlazaGroupSection, { props: { group: plazaGroup }, global })
      break
    }
    case 'AvailableChannelsTable': {
      const rows: UserAvailableChannel[] = [{ name: 'Channel', description: '', platforms: [
        { platform: 'openai', groups: [group], supported_models: [] }
      ] }]
      wrapper = shallowMount(AvailableChannelsTable, { props: {
        columns: { name: 'Channel', description: 'Description', platform: 'Platform', groups: 'Groups', supportedModels: 'Models' },
        rows, loading: false, pricingKeyPrefix: 'availableChannels.pricing', noPricingLabel: '', noModelsLabel: '',
        emptyLabel: '', userGroupRates: {}, serverUtcOffset: settings.cachedPublicSettings.server_utc_offset
      }, global })
      break
    }
  }
  await flushPromises()
  return {
    wrapper,
    async changeOffset(offset?: string) {
      settings.cachedPublicSettings.server_utc_offset = offset
      if (consumer === 'AvailableChannelsTable') await wrapper.setProps({ serverUtcOffset: offset })
      await flushPromises()
    },
    async changeWindow(enabled: boolean, end: string) {
      group.peak_rate_enabled = plan.peak_rate_enabled = enabled
      group.peak_end = plan.peak_end = end
      if (consumer === 'GroupBadge' || consumer === 'GroupOptionItem') await wrapper.setProps(badgeProps())
      if (consumer === 'PlazaGroupSection') await wrapper.setProps({ group: { ...group, models: [] } })
      await flushPromises()
    }
  }
}

enableAutoUnmount(afterEach)
const unexpectedHttp: Array<ReturnType<typeof vi.spyOn>> = []
beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  settings.cachedPublicSettings.server_utc_offset = '+05:30'
  route.query = {}
  unexpectedHttp.length = 0
  for (const method of ['get', 'post', 'put', 'delete'] as const) {
    unexpectedHttp.push(vi.spyOn(apiClient, method).mockRejectedValue(new Error('Unexpected HTTP in peak pricing regression')))
  }
})
afterEach(() => {
  try {
    for (const request of unexpectedHttp) expect(request).not.toHaveBeenCalled()
    expect(settings.showError).not.toHaveBeenCalled()
  } finally {
    vi.restoreAllMocks()
  }
})

describe('pricing peak presentation through real application installers', () => {
  it('binds the single pricing owner to all four existing consumer ports', () => {
    const { get } = appBindings()
    expect(get(paymentsServicesKey).peakRate).toBe(peakRatePresentation)
    expect(get(catalogPresentationKey).value.peakRate).toBe(peakRatePresentation)
    expect(get(subscriptionPresentationKey).peakRate).toBe(peakRatePresentation)
    expect(get(groupPresentationKey).peakRate).toBe(peakRatePresentation)
  })

  it.each(consumers)('%s preserves zero, live server timezone, disabled and incomplete windows', async consumer => {
    const { wrapper, changeOffset, changeWindow } = await mountConsumer(consumer)
    expect(wrapper.text()).toContain('22:00-06:00 ×0 (UTC+05:30)')
    await changeOffset('-03:30')
    expect(wrapper.text()).toContain('22:00-06:00 ×0 (UTC-03:30)')
    expect(wrapper.text()).not.toContain('UTC+05:30')
    await changeOffset(undefined)
    expect(wrapper.text()).toContain('22:00-06:00 ×0')
    expect(wrapper.text()).not.toContain('UTC')
    await changeWindow(false, '06:00')
    expect(wrapper.text()).not.toContain('22:00')
    await changeWindow(true, '')
    expect(wrapper.text()).not.toContain('22:00')
    await changeWindow(true, '06:00')
    expect(wrapper.text()).toContain('22:00-06:00 ×0')
  })
})
