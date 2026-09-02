import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  listGroups,
  updateGroup,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  getAllIncludingInactive,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  updateGroup: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  getAllIncludingInactive: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      update: updateGroup,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAllIncludingInactive,
      getAll: vi.fn(),
      create: vi.fn(),
      delete: vi.fn(),
      duplicate: vi.fn(),
      updateSortOrders: vi.fn()
    },
    accounts: {
      list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 }),
      getAll: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const baseGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: '',
  platform: 'openai',
  status: 'active',
  is_exclusive: false,
  subscription_type: 'standard',
  rate_multiplier: 1,
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  default_validity_days: 0,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  batch_image_discount_multiplier: 1,
  batch_image_hold_multiplier: 1,
  allow_video_generation: false,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  fallback_group_id_on_no_account: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  models_list_config: undefined,
  sort_order: 10
}

// 42 是 openai 分组并配了兜底到 43；43 同平台。editForm 的初始平台是 anthropic，
// 所以打开 42 一定会触发 platform watcher——正是丢配置的那条路径。
const sourceGroup: AdminGroup = { ...baseGroup, fallback_group_id_on_no_account: 43 }
const fallbackGroup: AdminGroup = { ...baseGroup, id: 43, name: 'Spare', sort_order: 20 }

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})
const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})
const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView no-account fallback', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups,
      updateGroup,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAllIncludingInactive,
      showSuccess,
      showError
    ]) {
      fn.mockReset()
    }

    listGroups.mockResolvedValue({
      items: [sourceGroup, fallbackGroup],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    updateGroup.mockResolvedValue(sourceGroup)
    getModelsListCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    getAllIncludingInactive.mockResolvedValue([sourceGroup, fallbackGroup])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // 平台 watcher 是 pre-flush，跑在 handleEdit 的同步回填之后。无条件清空时这里
  // 会提交 0，后端按「清除兜底」处理——管理员什么都没改，配置就没了。
  it('keeps the configured fallback group when editing without touching it', async () => {
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    expect(editButton).toBeDefined()
    await editButton!.trigger('click')
    await flushPromises()
    await nextTick()
    await nextTick()

    const forms = wrapper.findAll('form')
    expect(forms.length).toBeGreaterThan(0)
    await forms[forms.length - 1].trigger('submit')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledTimes(1)
    expect(updateGroup.mock.calls[0][1]).toMatchObject({
      fallback_group_id_on_no_account: 43
    })
    wrapper.unmount()
  })

  // 分组列表是分页 + 筛选后的当前页。目标分组不在当前页时，下拉里根本没有它的
  // option：Select 退回显示占位「不兜底」，可值仍随保存提交，管理员也无法重选。
  it('offers the saved fallback target even when it is off the current page', async () => {
    listGroups.mockResolvedValue({
      items: [sourceGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    await editButton!.trigger('click')
    await flushPromises()
    await nextTick()

    const noAccountSelect = wrapper.findAllComponents({ name: 'Select' }).find(select => {
      const options = select.props('options') as { value: number | null; label: string }[] | undefined
      return options?.[0]?.label === 'admin.groups.noAccountFallback.noFallback'
    })
    expect(noAccountSelect).toBeDefined()
    expect(noAccountSelect!.props('options')).toEqual([
      { value: null, label: 'admin.groups.noAccountFallback.noFallback' },
      { value: 43, label: 'Spare' }
    ])
    wrapper.unmount()
  })
})
