import { peakRatePresentation } from '@/features/pricing'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper, VueWrapper } from '@vue/test-utils'

import RiskControlView from '../RiskControlPage.vue'
import type { ContentModerationAPIKeyStatus, ContentModerationConfig, UpdateContentModerationConfig } from '@/features/access-policy/api/riskControl'

const {
  getConfig,
  getSettings,
  updateSettings,
  updateConfig,
  getStatus,
  listLogs,
  getGroups,
  getProxies,
  testAPIKeys,
  clearFlaggedHashes,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  updateConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  getGroups: vi.fn(),
  getProxies: vi.fn(),
  testAPIKeys: vi.fn(),
  clearFlaggedHashes: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

const settingsApiFixture = vi.hoisted(() => ({ getSettings, updateSettings }))
const riskControlApiFixture = vi.hoisted(() => ({
      getConfig,
      updateConfig,
      getStatus,
      listLogs,
      testAPIKeys,
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes,
      unbanUser: vi.fn(),
    }))
const groupsApiFixture = vi.hoisted(() => ({
      getAll: getGroups,
    }))
const proxiesApiFixture = vi.hoisted(() => ({
      getAll: getProxies,
    }))
const accountsApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected accounts.list request in this test') }), getById: vi.fn(() => { throw new Error('Unexpected accounts.getById request in this test') }) }))
const usersApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected users.list request in this test') }) }))
vi.mock('@/features/system/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/system/api')>()
  return { ...actual, settingsAPI: settingsApiFixture }
})
vi.mock('@/features/resources/proxies', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/resources/proxies')>()
  return { ...actual, default: proxiesApiFixture }
})

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/shared/lib/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.riskControl.preBlockAPIKeyLoadSummary') {
          return `同步并发 ${params?.active} / 可用 Key ${params?.available}，累计 ${params?.total} 次，worker：${params?.workerActive} / ${params?.workerTotal}`
        }
        return key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`))
      },
    }),
  }
})

const baseConfig = (): ContentModerationConfig => ({
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  proxy_id: null,
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: '内容审计命中风险规则，请调整输入后重试',
  email_on_hit: true,
  auto_ban_enabled: true,
  ban_threshold: 10,
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: [],
  keyword_blocking_mode: 'keyword_and_api',
  thresholds: {
    harassment: 0.98,
    sexual: 0.65,
  },
  model_filter: {
    type: 'all',
    models: [],
  },
})

const runtimeStatus = () => ({
  enabled: true,
  risk_control_enabled: true,
  mode: 'pre_block',
  worker_count: 4,
  max_workers: 32,
  active_workers: 0,
  idle_workers: 4,
  queue_size: 32768,
  queue_length: 0,
  queue_usage_percent: 0,
  enqueued: 0,
  dropped: 0,
  processed: 0,
  errors: 0,
  pre_block_active: 0,
  pre_block_checked: 0,
  pre_block_allowed: 0,
  pre_block_blocked: 0,
  pre_block_errors: 0,
  pre_block_avg_latency_ms: 0,
  pre_block_api_key_active: 0,
  pre_block_api_key_available_count: 0,
  pre_block_api_key_total_calls: 0,
  pre_block_api_key_loads: [],
  api_key_statuses: [],
  flagged_hash_count: 0,
  last_cleanup_deleted_hit: 0,
  last_cleanup_deleted_non_hit: 0,
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const BaseDialogStub = defineComponent({
  props: {
    show: {
      type: Boolean,
      default: false,
    },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const ModelWhitelistSelectorStub = defineComponent({
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const onInput = (event: Event) => {
      const value = (event.target as HTMLInputElement).value
      emit(
        'update:modelValue',
        value
          .split(/[,\n]/)
          .map((item) => item.trim())
          .filter(Boolean)
      )
    }
    return () =>
      h('input', {
        'data-test': 'model-filter-input',
        value: (props.modelValue as string[]).join('\n'),
        onInput,
      })
  },
})

function findButtonByText(wrapper: VueWrapper, text: string): DOMWrapper<HTMLButtonElement> {
  const button = wrapper.findAll<HTMLButtonElement>('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`button not found: ${text}`)
  }
  return button
}

describe('admin RiskControlView', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue({ revision: 'risk-settings-revision' })
    updateSettings.mockReset().mockResolvedValue({ risk_control_enabled: true })
    getConfig.mockReset()
    updateConfig.mockReset()
    getStatus.mockReset()
    listLogs.mockReset()
    getGroups.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    testAPIKeys.mockReset()
    clearFlaggedHashes.mockReset()
    clearFlaggedHashes.mockResolvedValue({ deleted: 3 })

    getConfig.mockResolvedValue(baseConfig())
    getStatus.mockResolvedValue(runtimeStatus())
    listLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    getGroups.mockResolvedValue([])
    getProxies.mockResolvedValue([])
    updateConfig.mockImplementation(async (payload: UpdateContentModerationConfig) => ({
      ...baseConfig(),
      ...payload,
      model_filter: payload.model_filter ?? baseConfig().model_filter,
      api_key_configured: false,
      api_key_masked: '',
      api_key_count: 0,
      api_key_masks: [],
      api_key_statuses: [],
    }))
  })

  it('loads, edits and saves all guard settings through the existing config API', async () => {
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.guards').trigger('click')
    expect((wrapper.get('[data-test="refusal_guard-ttl"]').element as HTMLInputElement).value).toBe('3600')
    expect((wrapper.get('[data-test="device_guard-ttl"]').element as HTMLInputElement).value).toBe('900')
    await wrapper.get('[data-test="failure-policy"]').setValue('fail_closed')
    await wrapper.get('[data-test="distillation-enabled"]').setValue(true)
    await wrapper.get('[data-test="distillation-phrases"]').setValue('custom phrase\nsecond phrase')
    await wrapper.get('[data-test="distillation-patterns"]').setValue('(?i)dataset.*collection')
    await wrapper.get('[data-test="distillation-fingerprints"]').setValue('a'.repeat(64))
    await wrapper.get('[data-test="distillation-structure"]').setValue('challenge')
    await wrapper.get('[data-test="distillation-max-tokens"]').setValue(16384)
    await wrapper.get('[data-test="distillation-thinking-tokens"]').setValue(4096)
    await wrapper.get('[data-test="regex-rules"]').setValue('blocked.*pattern')
    await wrapper.get('[data-test="refusal_guard-enabled"]').setValue(true)
    await wrapper.get('[data-test="device_guard-enabled"]').setValue(true)
    await wrapper.get('[data-test="refusal_guard-ttl"]').setValue(600)
    await wrapper.get('[data-test="refusal_guard-capacity"]').setValue(50)
    await wrapper.get('[data-test="device_guard-ttl"]').setValue(120)
    await wrapper.get('[data-test="device_guard-capacity"]').setValue(10)
    await wrapper.get('[data-test="refusal-similarity"]').setValue(0.95)
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      failure_policy: 'fail_closed',
      distillation: { enabled: true, phrases: ['custom phrase', 'second phrase'], patterns: ['(?i)dataset.*collection'], fingerprints: ['a'.repeat(64)], structure_pattern: 'challenge', min_max_tokens: 16384, min_thinking_tokens: 4096 },
      regex_rules: ['blocked.*pattern'],
      refusal_guard: { enabled: true, ttl_seconds: 600, max_entries: 50, similarity_threshold: 0.95 },
      device_guard: { enabled: true, ttl_seconds: 120, max_entries: 10 },
    }))
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.guards').trigger('click')
    expect((wrapper.get('[data-test="regex-rules"]').element as HTMLTextAreaElement).value).toBe('blocked.*pattern')
    expect(updateSettings).not.toHaveBeenCalled()
    // Explicit removal stays empty and failed saves retain the user's draft.
    await wrapper.get('[data-test="regex-rules"]').setValue('')
    updateConfig.mockRejectedValueOnce(new Error('validation failed'))
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="risk-guards"]').exists()).toBe(true)
    expect(updateConfig.mock.lastCall?.[0].regex_rules).toEqual([])
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['success', 'config failure', 'enable failure'])('saves rules before explicitly enabling the global gate: %s', async (outcome) => {
    getConfig.mockResolvedValue({ ...baseConfig(), enabled: false, mode: 'off' })
    getStatus.mockResolvedValue({ ...runtimeStatus(), enabled: false, risk_control_enabled: false })
    if (outcome === 'config failure') updateConfig.mockRejectedValueOnce(new Error('config failed'))
    if (outcome === 'enable failure') updateSettings.mockRejectedValueOnce(new Error('enable failed'))
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await wrapper.get('[data-test="save-and-enable"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, mode: 'pre_block' }))
    if (outcome === 'config failure') {
      expect(updateSettings).not.toHaveBeenCalled()
    } else {
      expect(updateSettings).toHaveBeenCalledWith({ risk_control_enabled: true, revision: 'risk-settings-revision' })
      expect(getSettings).toHaveBeenCalledOnce()
      expect(updateConfig.mock.invocationCallOrder[0]).toBeLessThan(getSettings.mock.invocationCallOrder[0]!)
      expect(getSettings.mock.invocationCallOrder[0]).toBeLessThan(updateSettings.mock.invocationCallOrder[0]!)
      expect(updateConfig.mock.invocationCallOrder[0]).toBeLessThan(updateSettings.mock.invocationCallOrder[0]!)
    }
    if (outcome !== 'success') {
      expect(showSuccess).not.toHaveBeenCalled()
      expect(wrapper.find('[data-test="save-and-enable"]').exists()).toBe(true)
      if (outcome === 'enable failure') expect(showError).toHaveBeenCalledWith('admin.riskControl.savedButNotEnabled')
    }
    wrapper.unmount()
  })

  it('clears temporary guard memory even when the legacy hash count is zero', async () => {
    getStatus.mockResolvedValue({ ...runtimeStatus(), refusal_guard_entries: 2, device_guard_entries: 1 })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.guards').trigger('click')
    await wrapper.get('[data-test="clear-guard-memory"]').trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledWith('admin.riskControl.guard.clearConfirm')
    expect(clearFlaggedHashes).toHaveBeenCalledOnce()
    confirm.mockRestore()
    wrapper.unmount()
  })

  it.each(['openai', 'typesafe'] as const)('shows the selected draft engine keys while %s remains active', async (activeEngine) => {
    const openaiKey: ContentModerationAPIKeyStatus = {
      index: 1, key_hash: 'openai-hash', masked: '********oa01', status: 'frozen',
      failure_count: 1, success_count: 0, last_error: '', last_latency_ms: 100,
      last_http_status: 429, last_tested: false, configured: true,
    }
    const typesafeKey: ContentModerationAPIKeyStatus = { ...openaiKey, key_hash: 'typesafe-hash', masked: '********ts02', status: 'ok', failure_count: 0, success_count: 1, last_http_status: 200 }
    const profiles = {
      openai: { ...baseConfig(), engine: 'openai' as const, api_key_configured: true, api_key_count: 1, api_key_statuses: [openaiKey] },
      typesafe: { ...baseConfig(), engine: 'typesafe' as const, api_key_configured: true, api_key_count: 1, api_key_statuses: [typesafeKey] },
    }
    const otherEngine = activeEngine === 'openai' ? 'typesafe' : 'openai'
    getConfig.mockResolvedValue({ ...profiles[activeEngine], engine_configs: profiles })
    getStatus.mockResolvedValue({ ...runtimeStatus(), engine: activeEngine, api_key_statuses: profiles[activeEngine].api_key_statuses })
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    const engine = () => wrapper.findComponent('[data-test="audit-engine-select"]')
    const keyPanel = () => wrapper.get('[data-test="audit-key-statuses"]')
    expect(keyPanel().text()).toContain(profiles[activeEngine].api_key_statuses[0].masked)
    engine().vm.$emit('update:modelValue', otherEngine)
    await flushPromises()
    expect(keyPanel().text()).toContain(profiles[otherEngine].api_key_statuses[0].masked)
    expect(keyPanel().text()).not.toContain(profiles[activeEngine].api_key_statuses[0].masked)
    expect(keyPanel().text()).toContain(otherEngine === 'openai' ? 'admin.riskControl.apiKeyStatusFrozen' : 'admin.riskControl.apiKeyStatusOk')
    await findButtonByText(wrapper, 'admin.riskControl.refreshStatus').trigger('click')
    await flushPromises()
    expect(keyPanel().text()).toContain(profiles[otherEngine].api_key_statuses[0].masked)
    expect(keyPanel().text()).not.toContain(profiles[activeEngine].api_key_statuses[0].masked)
    engine().vm.$emit('update:modelValue', activeEngine)
    await flushPromises()
    expect(keyPanel().text()).toContain(profiles[activeEngine].api_key_statuses[0].masked)
    expect(keyPanel().text()).not.toContain(profiles[otherEngine].api_key_statuses[0].masked)
    expect(updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows an empty key panel for an unconfigured draft engine', async () => {
    getStatus.mockResolvedValue({ ...runtimeStatus(), api_key_statuses: [{ masked: '********oa01', configured: true, status: 'ok' }] })
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    wrapper.findComponent('[data-test="audit-engine-select"]').vm.$emit('update:modelValue', 'typesafe')
    await flushPromises()
    expect(wrapper.get('[data-test="audit-key-statuses"]').text()).toContain('admin.riskControl.apiKeyHealthEmpty')
    expect(wrapper.get('[data-test="audit-key-statuses"]').text()).not.toContain('********oa01')
    expect(updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves both engine drafts and saves thresholds independently', async () => {
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await wrapper.get('[data-test="audit-base-url"]').setValue('https://openai-edited.example')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('73')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.basic').trigger('click')
    const engine = () => wrapper.findComponent('[data-test="audit-engine-select"]')
    engine().vm.$emit('update:modelValue', 'typesafe')
    await flushPromises()
    expect((wrapper.get('[data-test="audit-model"]').element as HTMLInputElement).value).toBe('jev-latest')
    await wrapper.get('[data-test="audit-base-url"]').setValue('https://typesafe-edited.example')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    expect((wrapper.get('[data-test="risk-threshold-sexual"]').element as HTMLInputElement).value).toBe('65')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('91')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.basic').trigger('click')
    engine().vm.$emit('update:modelValue', 'openai')
    await flushPromises()
    expect((wrapper.get('[data-test="audit-base-url"]').element as HTMLInputElement).value).toBe('https://openai-edited.example')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ engine: 'openai', engine_configs: {
      openai: expect.objectContaining({ base_url: 'https://openai-edited.example', thresholds: expect.objectContaining({ sexual: 0.73 }) }),
      typesafe: expect.objectContaining({ base_url: 'https://typesafe-edited.example', thresholds: expect.objectContaining({ sexual: 0.91 }) }),
    } }))
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('tests the draft engine without switching the active engine', async () => {
    testAPIKeys.mockResolvedValue({ items: [{ status: 'ok', configured: false }], image_count: 0 })
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    wrapper.findComponent('[data-test="audit-engine-select"]').vm.$emit('update:modelValue', 'typesafe')
    await flushPromises()
    await wrapper.get('textarea[autocomplete="new-password"]').setValue('test-only-key')
    await findButtonByText(wrapper, 'admin.riskControl.testInputApiKeys').trigger('click')
    await flushPromises()
    expect(testAPIKeys).toHaveBeenCalledWith(expect.objectContaining({ engine: 'typesafe', api_keys: ['test-only-key'], model: 'jev-latest', thresholds: expect.objectContaining({ sexual: 0.65 }) }))
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="active-audit-engine"]').text()).not.toContain('TypeSafe AI')
    wrapper.unmount()
  })

  it.each(['openai', 'typesafe'] as const)('restores the shared defaults for %s without replacing saved thresholds on load', async (engine) => {
    getConfig.mockResolvedValue({ ...baseConfig(), engine, thresholds: { sexual: 0.8, harassment: 0.8 } })
    const wrapper = mount(RiskControlView, { global: { stubs: { AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true, ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true } } })
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    expect((wrapper.get('[data-test="risk-threshold-sexual"]').element as HTMLInputElement).value).toBe('80')
    await findButtonByText(wrapper, 'admin.riskControl.riskThresholdReset').trigger('click')
    const defaults = {
      harassment: 98, 'harassment/threatening': 90, hate: 65, 'hate/threatening': 65,
      illicit: 95, 'illicit/violent': 95, 'self-harm': 65, 'self-harm/intent': 85,
      'self-harm/instructions': 65, sexual: 65, 'sexual/minors': 65, violence: 95, 'violence/graphic': 95,
    }
    for (const [category, value] of Object.entries(defaults)) {
      expect((wrapper.get(`[data-test="risk-threshold-${category}"]`).element as HTMLInputElement).value).toBe(String(value))
    }
    if (engine === 'typesafe') expect(wrapper.text()).toContain('admin.riskControl.typeSafeThresholds')
    expect(updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('saves the selected model filter mode and models', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.modelFilterInclude').trigger('click')
    await wrapper.get('[data-test="model-filter-input"]').setValue('gpt-5.5, gpt-5.4')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      model_filter: {
        type: 'include',
        models: ['gpt-5.5', 'gpt-5.4'],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('submits edited risk control thresholds when saving moderation config', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('72')
    await wrapper.get('[data-test="risk-threshold-harassment"]').setValue('99')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      thresholds: expect.objectContaining({
        sexual: 0.72,
        harassment: 0.99,
      }),
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('describes worker runtime as async audit and pre-block record processing', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      mode: 'observe',
      processed: 12,
      queue_length: 2,
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.workerStatusHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('2 / 32,768')
  })

  it('shows pre-block synchronous moderation metrics separately from worker queue', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      pre_block_active: 2,
      pre_block_checked: 128,
      pre_block_allowed: 120,
      pre_block_blocked: 8,
      pre_block_errors: 1,
      pre_block_avg_latency_ms: 86,
      pre_block_api_key_active: 2,
      pre_block_api_key_available_count: 2,
      pre_block_api_key_total_calls: 128,
      active_workers: 3,
      worker_count: 7,
      pre_block_api_key_loads: [
        {
          index: 0,
          key_hash: 'hash-one',
          masked: 'sk-...one',
          status: 'ok',
          active: 1,
          total: 72,
          success: 70,
          errors: 2,
          avg_latency_ms: 84,
          last_latency_ms: 80,
          last_http_status: 200,
        },
        {
          index: 1,
          key_hash: 'hash-two',
          masked: 'sk-...two',
          status: 'ok',
          active: 1,
          total: 56,
          success: 56,
          errors: 0,
          avg_latency_ms: 90,
          last_latency_ms: 92,
          last_http_status: 200,
        },
      ],
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.workerStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('128')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('86 ms')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockAPIKeyLoad')
    expect(wrapper.text()).toContain('sk-...one')
    expect(wrapper.text()).toContain('sk-...two')
    expect(wrapper.text()).toContain('72')
    expect(wrapper.text()).toContain('56')
    expect(wrapper.text()).toContain('同步并发 2 / 可用 Key 2，累计 128 次，worker：3 / 7')

    const runtimeCards = wrapper.get('[data-test="pre-block-runtime-cards"]')
    const syncCard = wrapper.get('[data-test="pre-block-sync-card"]')
    const apiKeyLoadCard = wrapper.get('[data-test="pre-block-api-key-load-card"]')

    expect(runtimeCards.classes()).toEqual(expect.arrayContaining([
      'grid',
      'grid-cols-1',
      'xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]',
    ]))
    expect(syncCard.element.parentElement).toBe(runtimeCards.element)
    expect(apiKeyLoadCard.element.parentElement).toBe(runtimeCards.element)
    expect(syncCard.classes()).toContain('card')
    expect(apiKeyLoadCard.classes()).toContain('card')
    expect(syncCard.get('h2').text()).toBe('admin.riskControl.preBlockSyncStatus')
    expect(syncCard.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(apiKeyLoadCard.get('h2').text()).toBe('admin.riskControl.preBlockAPIKeyLoad')
    expect(apiKeyLoadCard.text()).toContain('admin.riskControl.preBlockAPIKeyLoadHint')
    expect(wrapper.get('[data-test="pre-block-api-key-load-list"]').classes()).toEqual(expect.arrayContaining([
      'max-h-[280px]',
      'overflow-y-auto',
    ]))
  })
})

// Existing behavior under the migrated full administrator role.
vi.mock('@/features/identity', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/identity')>()),
  useAuthStore: () => ({ isSuperAdmin: true, isAdmin: true, isSimpleMode: false, can: () => true })
}))


import { config as testConfig } from '@vue/test-utils'
import { ref as testRef, computed as testComputed } from 'vue'
import { groupPresentationKey } from '@/features/access-policy/presentation/context'
import { accessPolicyProxyPickerKey } from '@/features/access-policy/services'
import { platformLabel } from '@/features/catalog'

import { useAppStore as mockedApp } from '@/app/model/app'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, accessPolicyUsersKey, riskControlProxiesKey, riskControlSettingsKey, accessPolicyModelSelectorKey } from '@/features/access-policy/services'
vi.mock('@/features/access-policy/api/groups', async () => ({ groupsAPI: groupsApiFixture }))
vi.mock('@/features/access-policy/api/riskControl', async () => ({ riskControlAPI: riskControlApiFixture }))
const permissionFallback = { isSuperAdmin: true, isSimpleMode: false, can: () => true, canEditAdminField: () => true, editableAdminFields: (_schema: string, payload: object) => payload }
const mockedPermissions = new Proxy(permissionFallback, { get(target, key) { return readPermissions()[key as keyof typeof target] ?? target[key as keyof typeof target] } })
Object.assign(testConfig.global.provide, {
 [accessPolicyProxyPickerKey as symbol]: { name: 'ProxySelector', props: ['modelValue', 'proxies'], emits: ['update:modelValue'], template: '<div />' },
 [groupPresentationKey as symbol]: { peakRate: peakRatePresentation, platformLabel, serverUtcOffset: testRef(undefined), isSimpleMode: testComputed(() => mockedPermissions.isSimpleMode) },
 [accessPolicyScopeKey as symbol]: testRef('session:admin:test'),
 [accessPolicyNotificationsKey as symbol]: new Proxy({}, { get(_target, key) { return mockedApp()[key as 'showError' | 'showSuccess'] } }),
 [accessPolicyPermissionsKey as symbol]: mockedPermissions,
 [accessPolicyAccountsKey as symbol]: { search: async (keyword: string, params: object, signal: AbortSignal) => (await accountsApiFixture.list(1, 20, { search: keyword, ...params }, { signal })).items, getById: (id: number) => accountsApiFixture.getById(id) },
 [accessPolicyUsersKey as symbol]: { search: async (keyword: string) => (await usersApiFixture.list(1, 10, { search: keyword })).items },
 [riskControlProxiesKey as symbol]: () => proxiesApiFixture.getAll(),
 [riskControlSettingsKey as symbol]: { enable: async () => { const current = await settingsApiFixture.getSettings(); await settingsApiFixture.updateSettings({ risk_control_enabled: true, revision: current.revision || '' }) } },
 [accessPolicyModelSelectorKey as symbol]: { name: 'ModelWhitelistSelector', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }
})
import { useAuthStore as testAuth } from '@/features/identity'
function readPermissions() { return testAuth() }
