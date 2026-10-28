import ResourceGroupSelector from '../ResourceGroupSelector.vue'
import { accountFixture } from '../../model/__tests__/boundaryFixtures'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import { i18n } from '@/i18n'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import EditAccountModal from '../EditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'


const boundarySession = vi.hoisted(() => ({ value: 'test-session:admin:0' }))

const { showError, showSuccess, translate } = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
  translate: vi.fn((key: string) => key)
}))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn()
  })
}))

vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({
    isSimpleMode: true,
    isSuperAdmin: true,
    user: { id: 1, role: 'super_admin' }
  })
}))

const accountsApiFixture = vi.hoisted(() => ({
      list: vi.fn(),
      getById: vi.fn(),
      bulkUpdate: vi.fn(),
      checkMixedChannelRisk: vi.fn()
    }))



vi.mock('@/features/access-policy/groups', () => ({
  getModelAllowlistCandidates: vi.fn().mockImplementation(async (_id: number, platform: string) =>
    platform === 'antigravity' ? ['gemini-3.1-flash-image', 'gemini-3.8-flash-high'] : ['gpt-reserve'])
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: translate
    })
  }
})

function mountModal(extraProps: Record<string, unknown> = {}) {
  return mount(BulkEditAccountModal, {
    props: {
      show: true,
      accountIds: [1, 2],
      selectedPlatforms: ['antigravity'],
      selectedTypes: ['apikey'],
      proxies: [],
      groups: [],
      ...extraProps
    } as any,
    global: {
      provide: { [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } },
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        EditAccountModal: true,
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: `
            <select
              v-bind="$attrs"
              :value="modelValue"
              @change="$emit('update:modelValue', $event.target.value)"
            >
              <option v-for="option in options" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          `
        },
        ProxySelector: true,
        GroupSelector: true,
        Icon: true
      }
    }
  })
}

describe('BulkEditAccountModal', () => {
  it.each([false, true])('applies only the selected Cloud switch: %s', async enabled => {
    const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
    await wrapper.get('#bulk-edit-cloud-enabled').setValue(true)
    await flushPromises()
    if (enabled) await wrapper.get('#bulk-edit-cloud-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent'); await flushPromises()
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], { extra: { openai_cloud: enabled } })
    wrapper.unmount()
  })
  it.each([
    { auth_mode: 'pat' },
    { chatgpt_account_id: '' },
    { chatgpt_user_id: '' }
  ])('identifies ineligible accounts %j and still allows disabling Cloud', async invalid => {
    vi.mocked(accountsApiFixture.getById).mockImplementation(async id => ({ id, name: `account-${id}`, platform: 'openai', type: 'oauth', credentials: { chatgpt_account_id: 'account', chatgpt_user_id: 'user', ...(id === 2 ? invalid : {}) } }) as any)
    const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
    await wrapper.get('#bulk-edit-cloud-enabled').setValue(true)
    await flushPromises()
    expect(wrapper.get('#bulk-edit-cloud-toggle').attributes('disabled')).toBeDefined()
    expect(translate).toHaveBeenCalledWith('admin.accounts.openai.cloud.ineligibleAccounts', { accounts: 'account-2 #2' })
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent'); await flushPromises()
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], { extra: { openai_cloud: false } })
    wrapper.unmount()
  })

  it('leaves Cloud unchanged unless its batch field is selected', async () => {
    const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
    expect(wrapper.get('#bulk-edit-cloud-toggle').attributes('disabled')).toBeDefined()
    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent'); await flushPromises()
    await flushPromises()
    expect(vi.mocked(accountsApiFixture.bulkUpdate).mock.calls.at(-1)?.[1].extra).not.toHaveProperty('openai_cloud')
    await wrapper.setProps({ selectedTypes: ['apikey'] })
    expect(wrapper.find('#bulk-edit-cloud-enabled').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['anthropic', ['oauth', 'setup-token']],
    ['openai', ['oauth', 'apikey']]
  ])('uses the managed catalog for selected %s accounts without a legacy sync action', async (platform, types) => {
    const wrapper = mountModal({ selectedPlatforms: [platform], selectedTypes: types })
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    expect(selector.props('platforms')).toEqual([platform])
    expect(selector.props('syncAccountIds')).toBeUndefined()
    expect(selector.text()).not.toContain('admin.accounts.syncUpstreamModels')
  })

  it.each(['anthropic', 'openai'])('preserves frozen target IDs for %s managed allowlist saves', async (platform) => {
    const wrapper = mountModal({
      accountIds: [],
      target: { mode: 'selected', accountIds: [3, 4], selectedPlatforms: [platform], selectedTypes: ['apikey'] }
    })
    expect(wrapper.findComponent(ModelWhitelistSelector).props('platforms')).toEqual([platform])
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([3, 4], { credentials: { model_allowlist: null } })
    expect(accountsApiFixture.list).not.toHaveBeenCalled()
  })

  it('does not offer live sync for unsupported Anthropic Bedrock or Vertex accounts', () => {
    for (const type of ['bedrock', 'service_account']) {
      const wrapper = mountModal({ selectedPlatforms: ['anthropic'], selectedTypes: [type] })
      expect(wrapper.findComponent(ModelWhitelistSelector).props('syncAccountIds')).toBeUndefined()
      wrapper.unmount()
    }
  })

  it.each([
    { selectedPlatforms: ['openai', 'anthropic'], selectedTypes: ['apikey'] },
    { selectedPlatforms: ['openai'], selectedTypes: ['oauth', 'service_account'] }
  ])('hides bulk sync for mixed platforms or unsupported OpenAI types: %j', (props) => {
    const wrapper = mountModal(props)
    expect(wrapper.findComponent(ModelWhitelistSelector).props('syncAccountIds')).toBeUndefined()
  })

  beforeEach(() => {
    vi.mocked(accountsApiFixture.getById).mockReset()
    vi.mocked(accountsApiFixture.getById).mockImplementation(async id => ({ id, name: `account-${id}`, platform: 'openai', type: 'oauth', credentials: { chatgpt_account_id: 'account', chatgpt_user_id: 'user' } }) as any)
    vi.mocked(accountsApiFixture.bulkUpdate).mockReset()
    vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    translate.mockClear()

    vi.mocked(accountsApiFixture.bulkUpdate).mockResolvedValue({
      success: 2,
      failed: 0,
      results: []
    } as any)
    vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockResolvedValue({
      has_risk: false
    } as any)
  })

  it('批量修改倍率时提示自动同步账号需要先关闭同步', async () => {
    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="bulk-rate-sync-warning"]').exists()).toBe(false)
    await wrapper.get('#bulk-edit-rate-multiplier-enabled').setValue(true)

    expect(wrapper.get('[data-testid="bulk-rate-sync-warning"]').text()).toContain(
      'admin.accounts.bulkEdit.rateSyncWarning'
    )
  })

  it('后端拒绝修改同步账号倍率时展示专用错误', async () => {
    vi.mocked(accountsApiFixture.bulkUpdate).mockRejectedValueOnce({
      status: 409,
      reason: 'UPSTREAM_BILLING_RATE_SYNC_BULK_CONFLICT',
      metadata: { count: '2' },
      message: 'conflict'
    })
    const wrapper = mountModal()

    await wrapper.get('#bulk-edit-rate-multiplier-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.rateSyncConflict')
  })

  it('antigravity 白名单包含 Gemini 图片模型且过滤掉普通 GPT 模型', async () => {
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    expect(selector.exists()).toBe(true)
    await flushPromises()

    await selector.find('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('gemini-3.1-flash-image')
    expect(wrapper.text()).toContain('gemini-3.8-flash-high')
    expect(wrapper.text()).not.toContain('gpt-5.3-codex')
  })

  it('antigravity 映射预设包含图片映射并过滤 OpenAI 预设', async () => {
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)

    const mappingTab = wrapper.findAll('button').find((btn) => btn.text().includes('admin.accounts.modelMapping'))
    expect(mappingTab).toBeTruthy()
    await mappingTab!.trigger('click')

    expect(wrapper.text()).toContain(i18n.global.t('ui.passthroughModel', { model: '3.1-Flash-Image' }))
    expect(wrapper.text()).toContain('3-Pro-Image→3.1')
    expect(wrapper.text()).not.toContain('GPT-5.3 Codex Spark')
  })

  it('仅勾选模型限制且白名单留空时，应提交独立 model_allowlist 继承目录', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['anthropic'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_allowlist: null
      }
    })
  })

  it('全部目标为 Grok OAuth 时，官方主机 base_url 作为手动端点切换正常提交', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://api.x.ai/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://api.x.ai/v1'
      }
    })
  })

  it('所选全为 grok 时展示快捷端点，点击后填入并自动勾选 base_url', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    const presets = wrapper.findAll('[data-testid="grok-base-url-preset"]')
    expect(presets.length).toBe(5)

    // 第三个预设为区域 API (us-east-1.api.x.ai/v1)
    await presets[2].trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://us-east-1.api.x.ai/v1'
      }
    })
  })

  it('所选含非 grok 平台时不展示快捷端点', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok', 'anthropic'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.findAll('[data-testid="grok-base-url-preset"]').length).toBe(0)
  })

  it.each(['kimi', 'zhipu', 'deepseek', 'minimax'])('全部目标为 %s API Key 时展示请求头覆写', (platform) => {
    const wrapper = mountModal({
      selectedPlatforms: [platform],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-header-override-enabled').exists()).toBe(true)
  })

  it.each(['kimi', 'zhipu', 'deepseek', 'minimax'])('目标为 %s OAuth 时不展示请求头覆写', (platform) => {
    const wrapper = mountModal({
      selectedPlatforms: [platform],
      selectedTypes: ['oauth']
    })

    expect(wrapper.find('#bulk-edit-header-override-enabled').exists()).toBe(false)
  })

  it('全部目标为 Grok OAuth 时，第三方 base_url 正常提交', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://relay.example.com/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://relay.example.com/v1'
      }
    })
  })

  it('混合类型选择（含 apikey）时官方主机 base_url 不拦截', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['apikey', 'oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://api.x.ai/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://api.x.ai/v1'
      }
    })
  })

  it(i18n.global.t('ui.passthroughModel', { model: 'OpenAI 账号批量编辑可开启自动' }), async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑可开启 namespace 摊平兼容开关', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-flatten-namespaces-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-flatten-namespaces-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_responses_flatten_namespaces: true
      }
    })
  })

  it('namespace 摊平开关不对 setup-token 等非 OAuth 选择展示', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth', 'setup-token']
    })

    expect(wrapper.find('#bulk-edit-openai-flatten-namespaces-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 OAuth 专属 WS mode 字段（含 http_bridge）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('http_bridge')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_oauth_responses_websockets_v2_mode: 'http_bridge',
        openai_oauth_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI API Key 批量编辑不显示 WS mode 入口', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-openai-ws-mode-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only_allow_app_server 字段（需同时开启父开关）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 子开关从属于 codex_cli_only：必须同时批量开启父开关才写入
    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true,
        codex_cli_only_allow_app_server: true
      }
    })
  })

  it('未同时开启父开关时不应写入 codex_cli_only_allow_app_server', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 仅开启子开关、不批量设置父开关 codex_cli_only：不应写入孤立字段，也不应调用接口
    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).not.toHaveBeenCalled()
  })

  it('OpenAI API Key 批量编辑应提交 API Key 专属 WS mode 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-apikey-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-apikey-ws-mode-select"]').setValue('ctx_pool')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_apikey_responses_websockets_v2_mode: 'ctx_pool',
        openai_apikey_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI 支持类型展示长上下文设置，混合平台隐藏全部新增设置', () => {
    for (const selectedTypes of [['oauth'], ['setup-token'], ['apikey'], ['oauth', 'setup-token', 'apikey']]) {
      const wrapper = mountModal({
        selectedPlatforms: ['openai'],
        selectedTypes
      })
      expect(wrapper.find('#bulk-edit-openai-long-context-billing-enabled').exists()).toBe(true)
      wrapper.unmount()
    }

    const mixed = mountModal({
      selectedPlatforms: ['openai', 'anthropic'],
      selectedTypes: ['apikey']
    })
    expect(mixed.find('#bulk-edit-openai-long-context-billing-enabled').exists()).toBe(false)
    expect(mixed.find('#bulk-edit-openai-endpoint-capabilities-enabled').exists()).toBe(false)
    expect(mixed.find('#bulk-edit-openai-responses-mode-enabled').exists()).toBe(false)
  })

  it('端点能力与 Responses 路由仅对全部 OpenAI API Key 目标展示', () => {
    const apiKey = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })
    expect(apiKey.find('#bulk-edit-openai-endpoint-capabilities-enabled').exists()).toBe(true)
    expect(apiKey.find('#bulk-edit-openai-responses-mode-enabled').exists()).toBe(true)
    apiKey.unmount()

    const oauth = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })
    expect(oauth.find('#bulk-edit-openai-endpoint-capabilities-enabled').exists()).toBe(false)
    expect(oauth.find('#bulk-edit-openai-responses-mode-enabled').exists()).toBe(false)
  })

  it('长上下文设置独立启用并提交布尔值', async () => {
    const enabledWrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await enabledWrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await enabledWrapper.get('[data-testid="bulk-edit-openai-long-context-billing-toggle"]').trigger('click')
    await enabledWrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenLastCalledWith([1, 2], {
      extra: { openai_long_context_billing_enabled: true }
    })
    enabledWrapper.unmount()

    vi.mocked(accountsApiFixture.bulkUpdate).mockClear()
    const disabledWrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['setup-token']
    })
    await disabledWrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await disabledWrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: { openai_long_context_billing_enabled: false }
    })
  })

  it('端点能力默认值提交 null，表示恢复两个默认端点', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: { openai_capabilities: null }
    })
  })

  it('Responses 路由独立启用，auto 提交 null，强制模式提交明确值', async () => {
    const autoWrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })
    await autoWrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)
    await autoWrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenLastCalledWith([1, 2], {
      extra: { openai_responses_mode: null }
    })
    autoWrapper.unmount()

    vi.mocked(accountsApiFixture.bulkUpdate).mockClear()
    const forcedWrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })
    await forcedWrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)
    await forcedWrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]').setValue('force_responses')
    await forcedWrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: { openai_responses_mode: 'force_responses' }
    })
  })

  it('仅启用 Embeddings 时恢复 Responses 自动模式并精确提交联动字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]').setValue('force_chat_completions')
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').setValue(false)

    expect((wrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]').element as HTMLSelectElement).value)
      .toBe('auto')
    expect(wrapper.find('[data-testid="bulk-edit-openai-responses-mode-not-applicable"]').exists()).toBe(true)

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: { openai_capabilities: ['embeddings'] },
      extra: { openai_responses_mode: null }
    })
  })

  it('persists Seedance in a two-capability bulk update', async () => {
    const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['apikey'] })
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-embeddings"]').setValue(false)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-seedance"]').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: { openai_capabilities: ['chat_completions', 'seedance'] }
    })
  })

  it('关闭端点能力修改后 Responses 路由恢复独立可编辑', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').setValue(false)
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(false)
    await wrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)

    const select = wrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]')
    expect(select.attributes('disabled')).toBeUndefined()
    await select.setValue('force_responses')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: { openai_responses_mode: 'force_responses' }
    })
  })

  it('目标变化后不提交已经隐藏的 OpenAI 设置', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)
    await wrapper.setProps({ selectedPlatforms: ['anthropic'], selectedTypes: ['apikey'] })
    await wrapper.get('#bulk-edit-status-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      status: 'active'
    })
  })

  it('至少保留一个端点能力', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').setValue(false)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-embeddings"]').setValue(false)

    expect((wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-embeddings"]').element as HTMLInputElement).checked)
      .toBe(true)
  })

  it('关闭弹窗后重置新增设置的启用状态和值', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })
    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-long-context-billing-toggle"]').trigger('click')
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').setValue(false)
    await wrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)

    await wrapper.setProps({ show: false })
    await nextTick()

    expect((wrapper.get('#bulk-edit-openai-long-context-billing-enabled').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-testid="bulk-edit-openai-long-context-billing-toggle"]').attributes('aria-checked')).toBe('false')
    expect((wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]').element as HTMLSelectElement).value).toBe('auto')
  })

  it('筛选快照展示影子继承说明并按固定账号提交', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'selected',
        accountIds: [3, 4],
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth']
      }
    })

    expect(wrapper.get('[data-testid="bulk-edit-openai-long-context-shadow-hint"]').text())
      .toContain('admin.accounts.bulkEdit.longContextShadowHint')
    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-long-context-billing-toggle"]').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([3, 4], {
      extra: { openai_long_context_billing_enabled: true }
    })
  })

  it('成功响应包含影子继承数量时展示专用提示', async () => {
    vi.mocked(accountsApiFixture.bulkUpdate).mockResolvedValueOnce({
      success: 2,
      failed: 0,
      long_context_inherited_count: 1,
      results: []
    } as any)
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })
    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.bulkEdit.successWithInherited')
    expect(translate).toHaveBeenCalledWith('admin.accounts.bulkEdit.successWithInherited', {
      count: 2,
      inherited: 1
    })
  })

  it('部分成功且包含影子继承数量时展示组合提示', async () => {
    vi.mocked(accountsApiFixture.bulkUpdate).mockResolvedValueOnce({
      success: 1,
      failed: 1,
      long_context_inherited_count: 1,
      results: []
    } as any)
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })
    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.partialSuccessWithInherited')
    expect(translate).toHaveBeenCalledWith('admin.accounts.bulkEdit.partialSuccessWithInherited', {
      success: 1,
      failed: 1,
      inherited: 1
    })
  })

  it('全影子长上下文错误使用专用提示并保持弹窗打开', async () => {
    vi.mocked(accountsApiFixture.bulkUpdate).mockRejectedValueOnce({
      status: 400,
      reason: 'OPENAI_LONG_CONTEXT_PARENT_REQUIRED',
      message: 'select parent'
    })
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })
    await wrapper.get('#bulk-edit-openai-long-context-billing-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.longContextParentRequired')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('OpenAI API Key 批量编辑可统一开启上游倍率自动探测', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: true
    })
  })

  it('非 OpenAI 平台的 API Key 批量编辑同样可开启上游倍率自动探测', async () => {
    // 探测已放宽到全部 API-key 平台，混合平台选择只要求类型全为 apikey。
    const wrapper = mountModal({
      selectedPlatforms: ['grok', 'anthropic'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: true
    })
  })

  it('OpenAI API Key 批量编辑可统一关闭上游倍率自动探测', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-upstream-billing-auto-probe-select"]').setValue('disabled')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: false
    })
  })

  it('非 OpenAI API Key 目标不显示上游倍率自动探测批量开关', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    expect(wrapper.find('#bulk-edit-upstream-billing-auto-probe-enabled').exists()).toBe(false)
  })

  it('筛选结果批量编辑可统一开启上游倍率自动探测', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'selected',
        accountIds: [3, 4],
        selectedPlatforms: ['openai'],
        selectedTypes: ['apikey']
      }
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([3, 4], {
      upstream_billing_probe_enabled: true
    })
  })

  it('筛选 OpenAI 账号批量编辑应提交 Compact 模式和专属模型映射', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'selected',
        accountIds: [3, 4],
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth', 'apikey']
      }
    })

    await wrapper.get('#bulk-edit-openai-compact-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-mode-select"]').setValue('force_on')
    await wrapper.get('#bulk-edit-openai-compact-model-mapping-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-add"]').trigger('click')
    const inputs = wrapper.findAll('[data-testid="bulk-edit-openai-compact-model-mapping-input"]')
    await inputs[0].setValue('gpt-5.4')
    await inputs[1].setValue('gpt-5.4-openai-compact')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([3, 4], {
      extra: {
        openai_compact_mode: 'force_on'
      },
      credentials: {
        compact_model_mapping: {
          'gpt-5.4': 'gpt-5.4-openai-compact'
        }
      }
    })
  })

  it(i18n.global.t('ui.passthroughModel', { model: 'OpenAI 账号批量编辑可关闭自动' }), async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: false,
        openai_oauth_passthrough: false
      }
    })
  })

  it('开启 OpenAI 自动透传时不再同时提交模型限制', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
    expect(wrapper.text()).toContain('admin.accounts.openai.modelRestrictionDisabledByPassthrough')
  })

  it('uses the frozen OAuth selection for group changes even if live selection changes', async () => {
    const wrapper = mountModal({
      accountIds: [101, 102],
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      target: {
        mode: 'selected', accountIds: [101, 102],
        selectedPlatforms: ['openai'], selectedTypes: ['oauth']
      }
    })
    await wrapper.setProps({ accountIds: [101, 102, 201, 202] })
    await wrapper.get('#bulk-edit-groups-enabled').setValue(true)
    wrapper.findComponent({ name: 'GroupSelector' }).vm.$emit('update:modelValue', [11, 15])
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([101, 102], { group_ids: [11, 15] })
  })

  it('does not replace an empty frozen selection with live account IDs', async () => {
    const wrapper = mountModal({ target: { mode: 'selected', accountIds: [] } })
    await wrapper.get('#bulk-edit-status-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.noSelection')
  })

  // issue #6327：批量编辑无法把 Codex 指纹收敛关掉。
  //
  // 批量更新走 JSONB 顶层合并（extra = COALESCE(extra,'{}') || payload），删掉 payload
  // 里的键只表示「本次不更新该键」，清不掉账号已有的 device/session/full；而且只删不写会让
  // payload 退化成 {extra:{}}，被后端 len(req.Extra) > 0 判为空更新直接 400
  // "No updates provided"。Create/Edit 那两个表单能删键，是因为它们提交完整 extra 对象、
  // 后端整体 SetExtra 覆盖——两种持久化语义不能共用同一套写法。
  it('OpenAI OAuth 批量编辑选择「关闭」时应显式提交 codex_fingerprint_mode=off（issue #6327）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 下拉框默认就是 off，用户只勾选「编辑该项」即提交——正是 issue 描述的操作路径。
    await wrapper.get('#bulk-edit-openai-codex-fingerprint-mode-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_fingerprint_mode: 'off'
      }
    })

    // 缺陷时期的形状：extra 为空对象，后端必然回 400。显式钉死不得回退。
    const payload = vi.mocked(accountsApiFixture.bulkUpdate).mock.calls[0][1] as {
      extra: Record<string, unknown>
    }
    expect(Object.keys(payload.extra).length).toBeGreaterThan(0)
  })

  // 与兄弟字段 codex_cli_only 的写法对齐：关闭态同样落显式值，不靠省略表达。
  it('OpenAI OAuth 批量编辑显式 opt-in 模式仍原样提交', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-fingerprint-mode-enabled').setValue(true)
    await wrapper
      .get('[data-testid="bulk-codex-fingerprint-mode-select"]')
      .setValue('session')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_fingerprint_mode: 'session'
      }
    })
  })

  // 未勾选「编辑该项」时不得写入该键，否则批量编辑别的字段会顺手清掉账号的收敛设置。
  it('未勾选编辑该项时不写入 codex_fingerprint_mode', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true
      }
    })
  })
})

it('reuses the single editor and sends only changed operational fields in advanced bulk mode', async () => {
  const account = { id: 1, name: 'A', platform: 'openai', type: 'oauth', concurrency: 10, priority: 1, credentials: {}, extra: {} }
  vi.mocked(accountsApiFixture.getById).mockResolvedValueOnce(account as any)
  vi.mocked(accountsApiFixture.bulkUpdate).mockResolvedValueOnce({ success: 2, failed: 0, results: [] })
  const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
  await wrapper.get('[data-testid=bulk-full-editor]').trigger('click')
  await flushPromises()
  const editor = wrapper.getComponent(EditAccountModal)
  editor.vm.$emit('baseline', { concurrency: 10, priority: 1, extra: { openai_long_context_billing_enabled: false } })
  await flushPromises()
  await editor.props('saveAccount')!(1, { concurrency: 23, priority: 1, extra: { openai_long_context_billing_enabled: false } })
  expect(accountsApiFixture.bulkUpdate).toHaveBeenLastCalledWith([1, 2], { concurrency: 23 }, { view: 'management' })
  expect(editor.props('allowAuthorize')).toBe(false)
  wrapper.unmount()
})

vi.mock('@/features/resources/accounts', () => ({ ...accountsApiFixture, default: accountsApiFixture, accountsAPI: accountsApiFixture }))

vi.mock('@/features/resources/services', async () => {
const actual = await vi.importActual<typeof import('@/features/resources/services')>('@/features/resources/services')
const { editorTestServices } = await import('../../__tests__/editorTestServices')
const { getPresetMappingsByPlatform } = await import('@/features/catalog')

const { useAppStore } = await import('@/app/model/app')
const { useAuthStore } = await import('@/features/identity')
const { getModelAllowlistCandidates } = await import('@/features/access-policy/groups')
return { ...actual, useResourceEditorServices: () => { const services = editorTestServices()
services.scopeKey = () => boundarySession.value
services.modelPresets = getPresetMappingsByPlatform
services.notices = { ...services.notices, ...useAppStore() }
const auth = useAuthStore()
services.access = { canManage: true, get isSuperAdmin() { return auth.isSuperAdmin }, get isSimpleMode() { return auth.isSimpleMode }, get canAuthorize() { return typeof auth.can === 'function' ? auth.can('accounts.authorize') : true } }
services.managedModelCatalog = async (platform) => (await getModelAllowlistCandidates(0, platform)).map(id => ({ id, enabled: true, display_name: id }))






return services } }
})


// Regression coverage for supplier selection and field-permission boundaries.
it('does not retarget a pending bulk confirmation to a new selection', async () => {
  vi.mocked(accountsApiFixture.bulkUpdate).mockReset().mockRejectedValueOnce({ status: 409, error: 'mixed_channel_warning', message: 'fixture risk' }).mockResolvedValue({ success: 1, failed: 0, results: [] } as any)
  vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockResolvedValue({ has_risk: false } as any)
  const wrapper = mountModal()
  try {
    await flushPromises()
    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue(7)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(vi.mocked(accountsApiFixture.bulkUpdate).mock.calls[0][0]).toEqual([1, 2])
    expect(wrapper.getComponent({ name: 'ConfirmDialog' }).attributes('show')).toBe('true')
    await wrapper.setProps({ accountIds: [9] })
    wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(vi.mocked(accountsApiFixture.bulkUpdate).mock.calls).toEqual([[[1, 2], { concurrency: 7 }]])
  } finally { wrapper.unmount() }
})

it('omits hidden bulk model-mapping drafts after authorization is revoked', async () => {
  vi.mocked(accountsApiFixture.bulkUpdate).mockReset().mockResolvedValue({ success: 2, failed: 0, results: [] } as any)
  vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockResolvedValue({ has_risk: false } as any)
  const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['apikey'] })
  try {
    await flushPromises()
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue(7)
    await wrapper.setProps({ allowAuthorize: false })
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(vi.mocked(accountsApiFixture.bulkUpdate).mock.calls[0][1]).not.toHaveProperty('credentials')
  } finally { wrapper.unmount() }
})


describe('bound bulk submissions', () => {
  const wrappers: ReturnType<typeof mountModal>[] = []
  const mountBoundary = (props: Record<string, unknown> = {}) => {
    const wrapper = mountModal(props)
    wrappers.push(wrapper)
    return wrapper
  }
  const result = { success: 2, failed: 0, results: [] }
  const warning = { status: 409, error: 'mixed_channel_warning', message: 'fixture risk' }
  const confirm = (wrapper: ReturnType<typeof mountModal>) => wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
  const concurrencyDraft = async (wrapper: ReturnType<typeof mountModal>) => {
    await flushPromises()
    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue(7)
  }
  const submit = async (wrapper: ReturnType<typeof mountModal>) => {
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
  }
  beforeEach(() => {
    boundarySession.value = 'test-session:admin:0'
    showError.mockClear(); showSuccess.mockClear()
    vi.mocked(accountsApiFixture.bulkUpdate).mockReset().mockResolvedValue(result)
    vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockReset().mockResolvedValue({ has_risk: false })
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    boundarySession.value = 'test-session:admin:0'
  })

  it.each(['frozen selection', 'platform', 'type', 'authorize', 'transport', 'rate', 'supplier', 'writer', 'session', 'restored selection', 'restored permission', 'restored writer', 'reopened editor'])(
    'revokes a pending 409 confirmation after %s changes', async transition => {
      const writer = vi.fn().mockRejectedValueOnce(warning).mockResolvedValue(result)
      const replacement = vi.fn().mockResolvedValue(result)
      const wrapper = mountBoundary({ saveBatch: writer })
      await concurrencyDraft(wrapper); await submit(wrapper)
      expect(writer).toHaveBeenCalledTimes(1)
      if (transition === 'frozen selection') await wrapper.setProps({ target: { mode: 'selected', accountIds: [9], selectedPlatforms: ['antigravity'], selectedTypes: ['apikey'] } })
      if (transition === 'platform') await wrapper.setProps({ selectedPlatforms: ['anthropic'] })
      if (transition === 'type') await wrapper.setProps({ selectedTypes: ['oauth'] })
      if (transition === 'authorize') await wrapper.setProps({ allowAuthorize: false })
      if (transition === 'transport') await wrapper.setProps({ allowTransportEdit: false })
      if (transition === 'rate') await wrapper.setProps({ allowRateEdit: false })
      if (transition === 'supplier') await wrapper.setProps({ supplierScope: true })
      if (transition === 'writer') await wrapper.setProps({ saveBatch: replacement })
      if (transition === 'session') boundarySession.value = 'new-session:supplier:9'
      if (transition === 'restored selection') { await wrapper.setProps({ accountIds: [9] }); await wrapper.setProps({ accountIds: [1, 2] }) }
      if (transition === 'restored permission') { await wrapper.setProps({ allowAuthorize: false }); await wrapper.setProps({ allowAuthorize: true }) }
      if (transition === 'restored writer') { await wrapper.setProps({ saveBatch: replacement }); await wrapper.setProps({ saveBatch: writer }) }
      if (transition === 'reopened editor') { await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }) }
      confirm(wrapper); await flushPromises()
      expect(writer).toHaveBeenCalledTimes(1)
      expect(replacement).not.toHaveBeenCalled()
      expect(accountsApiFixture.bulkUpdate).not.toHaveBeenCalled()
    }
  )

  it.each(['writer', 'permission'])('does not dispatch after a %s change while preflight is pending', async transition => {
    let finish!: (value: { has_risk: boolean }) => void
    vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const writer = vi.fn().mockResolvedValue(result)
    const replacement = vi.fn().mockResolvedValue(result)
    const wrapper = mountBoundary({ saveBatch: writer })
    await concurrencyDraft(wrapper)
    await wrapper.get('#bulk-edit-groups-enabled').setValue(true)
    wrapper.getComponent(ResourceGroupSelector).vm.$emit('update:modelValue', [3])
    await submit(wrapper)
    expect(accountsApiFixture.checkMixedChannelRisk).toHaveBeenCalledWith({ platform: 'antigravity', group_ids: [3] })
    if (transition === 'writer') await wrapper.setProps({ saveBatch: replacement })
    else await wrapper.setProps({ allowAuthorize: false })
    finish({ has_risk: false }); await flushPromises()
    expect(writer).not.toHaveBeenCalled(); expect(replacement).not.toHaveBeenCalled()
  })

  it.each(['409', 'preflight'])('confirms the original detached payload and IDs after %s, not later drafts', async mode => {
    const writer = vi.fn().mockResolvedValue(result)
    if (mode === '409') writer.mockRejectedValueOnce(warning)
    else vi.mocked(accountsApiFixture.checkMixedChannelRisk).mockResolvedValueOnce({ has_risk: true })
    const groups = [3]
    const wrapper = mountBoundary({ saveBatch: writer })
    await concurrencyDraft(wrapper)
    await wrapper.get('#bulk-edit-groups-enabled').setValue(true)
    wrapper.getComponent(ResourceGroupSelector).vm.$emit('update:modelValue', groups)
    await submit(wrapper)
    groups.push(15)
    await wrapper.get('#bulk-edit-concurrency').setValue(23)
    confirm(wrapper); await flushPromises()
    const confirmed = [[1, 2], { concurrency: 7, group_ids: [3], confirm_mixed_channel_risk: true }]
    expect(writer.mock.calls).toEqual(mode === '409'
      ? [[[1, 2], { concurrency: 7, group_ids: [3] }], confirmed]
      : [confirmed])
    confirm(wrapper); await flushPromises()
    expect(writer).toHaveBeenCalledTimes(mode === '409' ? 2 : 1)
  })

  it.each(['cancel', 'unknown result'])('does not replay after %s', async state => {
    const writer = vi.fn().mockRejectedValueOnce(state === 'cancel' ? warning : new Error('result unknown'))
    const wrapper = mountBoundary({ saveBatch: writer })
    await concurrencyDraft(wrapper); await submit(wrapper)
    if (state === 'cancel') wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('cancel')
    confirm(wrapper); await flushPromises()
    expect(writer).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it('consumes confirmation once while its request is still pending', async () => {
    let finish!: (value: typeof result) => void
    const writer = vi.fn().mockRejectedValueOnce(warning).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountBoundary({ saveBatch: writer })
    await concurrencyDraft(wrapper); await submit(wrapper)
    confirm(wrapper); confirm(wrapper); await flushPromises()
    expect(writer).toHaveBeenCalledTimes(2)
    finish(result); await flushPromises()
    expect(writer).toHaveBeenCalledTimes(2)
  })

  it('does not publish a late write result into a new selection', async () => {
    let finish!: (value: typeof result) => void
    const writer = vi.fn().mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mountBoundary({ saveBatch: writer })
    await concurrencyDraft(wrapper); await submit(wrapper)
    await wrapper.setProps({ accountIds: [9] })
    finish(result); await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('keeps explicitly editable base_url while omitting hidden authorization drafts', async () => {
    const wrapper = mountBoundary({ selectedPlatforms: ['openai'], selectedTypes: ['apikey'] })
    await concurrencyDraft(wrapper)
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://relay.example.invalid/v1')
    await wrapper.setProps({ allowAuthorize: false })
    await submit(wrapper)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], { concurrency: 7, credentials: { base_url: 'https://relay.example.invalid/v1' } })
  })

  it('filters revoked transport/rate drafts while preserving an authorized empty model mapping', async () => {
    const wrapper = mountBoundary({ selectedPlatforms: ['openai'], selectedTypes: ['apikey'] })
    await concurrencyDraft(wrapper)
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://relay.example.invalid/v1')
    await wrapper.get('#bulk-edit-rate-multiplier-enabled').setValue(true)
    await wrapper.setProps({ allowTransportEdit: false, allowRateEdit: false })
    await submit(wrapper)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], { concurrency: 7, credentials: { model_allowlist: null } })
  })

  it('preserves explicit proxy, load-factor and group clearing values', async () => {
    const wrapper = mountBoundary()
    await flushPromises()
    await wrapper.get('#bulk-edit-proxy-enabled').setValue(true)
    await wrapper.get('#bulk-edit-load-factor-enabled').setValue(true)
    await wrapper.get('#bulk-edit-groups-enabled').setValue(true)
    await submit(wrapper)
    expect(accountsApiFixture.bulkUpdate).toHaveBeenCalledWith([1, 2], { proxy_id: 0, load_factor: 0, group_ids: [] })
  })

  it('rejects an old advanced-editor callback after a different selection opens', async () => {
    const load = vi.fn().mockResolvedValueOnce(accountFixture({ id: 1, platform: 'openai', concurrency: 10, extra: {} }))
      .mockResolvedValueOnce(accountFixture({ id: 9, platform: 'openai', concurrency: 10, extra: {} }))
    const writer = vi.fn().mockResolvedValue(result)
    const wrapper = mountBoundary({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'], loadReferenceAccount: load, saveBatch: writer })
    await wrapper.get('[data-testid=bulk-full-editor]').trigger('click'); await flushPromises()
    let editor = wrapper.getComponent(EditAccountModal)
    editor.vm.$emit('baseline', { concurrency: 10 }); await flushPromises()
    const oldSave = editor.props('saveAccount')!
    await wrapper.setProps({ accountIds: [9] })
    await wrapper.get('[data-testid=bulk-full-editor]').trigger('click'); await flushPromises()
    editor = wrapper.getComponent(EditAccountModal)
    editor.vm.$emit('baseline', { concurrency: 10 }); await flushPromises()
    await expect(oldSave(1, { concurrency: 23 })).rejects.toMatchObject({ name: 'StaleResourceTask' })
    expect(writer).not.toHaveBeenCalled()
  })

  it('preserves the custom advanced bulk writer and its management flag', async () => {
    const writer = vi.fn().mockResolvedValue(result)
    const load = vi.fn().mockResolvedValue(accountFixture({ id: 1, platform: 'openai', concurrency: 10, extra: {} }))
    const wrapper = mountBoundary({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'], loadReferenceAccount: load, saveBatch: writer })
    await wrapper.get('[data-testid=bulk-full-editor]').trigger('click'); await flushPromises()
    const editor = wrapper.getComponent(EditAccountModal)
    editor.vm.$emit('baseline', { concurrency: 10 }); await flushPromises()
    await editor.props('saveAccount')!(1, { concurrency: 23 })
    expect(writer).toHaveBeenCalledWith([1, 2], { concurrency: 23 }, true)
    expect(accountsApiFixture.bulkUpdate).not.toHaveBeenCalled()
  })
})
