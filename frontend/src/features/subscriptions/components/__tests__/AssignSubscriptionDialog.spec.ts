import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref, type App } from 'vue'
import type { Group } from '@/features/access-policy/api/groupTypes'
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import { subscriptionCanManageKey, subscriptionDirectoryKey, subscriptionNotificationsKey, subscriptionPresentationKey, subscriptionScopeKey, type SubscriptionDirectory } from '../../ports'
import AssignSubscriptionDialog from '../AssignSubscriptionDialog.vue'

const { assign, bulkAssign, showError, showSuccess, searchAssignableUsers } = vi.hoisted(() => ({
  assign: vi.fn(), bulkAssign: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
  searchAssignableUsers: vi.fn<SubscriptionDirectory['searchAssignableUsers']>()
}))
vi.mock('../../api/admin', () => ({ subscriptionsAPI: { assign, bulkAssign } }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

function group(overrides: Partial<Group> = {}): Group {
  return {
    id: 7, name: 'Subscription group', description: null, platform: 'openai', rate_multiplier: 0,
    is_exclusive: false, status: 'active', subscription_type: 'subscription',
    daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, long_context_pricing_enabled: false,
    allow_image_generation: false, allow_batch_image_generation: false, image_count_billing: false,
    image_rate_independent: false, image_rate_multiplier: 1, batch_image_discount_multiplier: 1, batch_image_hold_multiplier: 1,
    image_price_1k: null, image_price_2k: null, image_price_4k: null, video_rate_independent: false, video_rate_multiplier: 1,
    video_price_480p: null, video_price_720p: null, video_price_1080p: null, web_search_price_per_call: null,
    search_price_per_1k: null, audio_realtime_price_per_min: null, audio_tts_price_per_million_chars: null, audio_stt_price_per_hour: null,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, claude_code_only: false,
    fallback_group_id: null, fallback_group_id_on_invalid_request: null, allow_live: false, require_oauth_only: false, require_privacy_set: false,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...overrides
  }
}

function mountDialog(allowed = true) {
  const scope = ref('admin:1')
  const canManage = ref(allowed)
  const wrapper = mount(AssignSubscriptionDialog, {
    props: { groups: [group(), group({ id: 8, name: 'Inactive', status: 'inactive' }), group({ id: 9, name: 'Standard', subscription_type: 'standard' })] },
    global: {
      plugins: [{ install(app: App) {
        app.provide(subscriptionScopeKey, scope)
        app.provide(subscriptionCanManageKey, canManage)
        app.provide(subscriptionNotificationsKey, { showError, showSuccess })
        app.provide(subscriptionDirectoryKey, { listGroups: vi.fn().mockResolvedValue([]), searchAssignableUsers, searchHistoryUsers: vi.fn().mockResolvedValue([]) })
        app.provide(subscriptionPresentationKey, {
          peakRate: peakRatePresentation, groupPlatformOptions: ref([]), components: { GroupBadge, GroupOptionItem },
          serverUtcOffset: ref(undefined), platformLabel: platform => platform
        })
      } }],
      stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, GroupBadge: true, GroupOptionItem: true, Icon: true, Teleport: true }
    }
  })
  return { wrapper, scope, canManage }
}

async function selectTargets(wrapper: ReturnType<typeof mountDialog>['wrapper']) {
  await wrapper.get('button.select-trigger').trigger('click')
  await wrapper.get('[role="option"]').trigger('click')
  const search = wrapper.get('[data-assign-user-search] input')
  await search.setValue('user11')
  await search.trigger('focus')
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
  await wrapper.get('[data-assign-user-search] button').trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  searchAssignableUsers.mockResolvedValue([{ id: 11, email: 'user11@example.com' }])
})
afterEach(() => { vi.useRealTimers() })

describe('assignment group Select slots', () => {
  it('uses actual Select options and selected slots with domain fields, zero rate and null description', async () => {
    const { wrapper } = mountDialog()
    expect(wrapper.get('button.select-trigger').text()).toContain('admin.subscriptions.selectGroup')
    await wrapper.get('button.select-trigger').trigger('click')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1)
    expect(wrapper.getComponent(GroupOptionItem).props()).toMatchObject({ name: 'Subscription group', platform: 'openai', subscriptionType: 'subscription', rateMultiplier: 0, description: null, selected: false })
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.getComponent(GroupBadge).props()).toMatchObject({ name: 'Subscription group', platform: 'openai', subscriptionType: 'subscription', rateMultiplier: 0 })
    await wrapper.get('button.select-trigger').trigger('click')
    expect(wrapper.getComponent(GroupOptionItem).props('selected')).toBe(true)
    expect(assign).not.toHaveBeenCalled()
  })

  it('submits the selected user and group once while pending, then emits completion', async () => {
    let complete: (value: unknown) => void = () => { throw new Error('Assignment not started') }
    assign.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const { wrapper } = mountDialog()
    await selectTargets(wrapper)
    const form = wrapper.get('#assign-subscription-form')
    await form.trigger('submit')
    await form.trigger('submit')
    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith({ user_id: 11, group_id: 7, validity_days: 30 })
    expect(bulkAssign).not.toHaveBeenCalled()
    complete({ id: 1 })
    await flushPromises()
    expect(wrapper.emitted('completed')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalledWith('admin.subscriptions.subscriptionAssigned')
  })

  it('keeps read-only callers outside the assignment form', async () => {
    const { wrapper } = mountDialog(false)
    expect(wrapper.find('#assign-subscription-form').exists()).toBe(false)
    await wrapper.get('button[type="submit"]').trigger('click')
    expect(assign).not.toHaveBeenCalled()
    expect(bulkAssign).not.toHaveBeenCalled()
  })

  it('requires a new explicit user selection after editing the search text', async () => {
    const { wrapper } = mountDialog()
    await selectTargets(wrapper)
    await wrapper.get('[data-assign-user-search] input').setValue('someone else')
    await wrapper.get('#assign-subscription-form').trigger('submit')
    expect(assign).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.subscriptions.pleaseSelectUser')
  })

  it('ignores a completed assignment after the owning scope changes', async () => {
    let complete: (value: unknown) => void = () => { throw new Error('Assignment not started') }
    assign.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const { wrapper, scope } = mountDialog()
    await selectTargets(wrapper)
    await wrapper.get('#assign-subscription-form').trigger('submit')
    scope.value = 'admin:2'
    complete({ id: 1 })
    await flushPromises()
    expect(wrapper.emitted('completed')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith({ user_id: 11, group_id: 7, validity_days: 30 })
  })
})
