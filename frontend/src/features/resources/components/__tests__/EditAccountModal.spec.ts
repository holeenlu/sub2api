import { uiFeedbackKey } from '@/shared/ui/feedback'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/features/catalog'

const { updateAccountMock, checkMixedChannelRiskMock, authIsSimpleMode, authCanMock, authFieldMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  authIsSimpleMode: { value: true },
  authCanMock: vi.fn((_permission: string) => true),
  authFieldMock: vi.fn((_schema: string, _field: string) => true)
}))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({
    can: authCanMock,
    canEditAdminField: authFieldMock,
    isSuperAdmin: true,
    user: { id: 1, role: 'super_admin' },
    get isSimpleMode() {
      return authIsSimpleMode.value
    }
  })
}))

const accountsApiFixture = vi.hoisted(() => ({
      update: updateAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    }))
const settingsApiFixture = vi.hoisted(() => ({
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    }))
const tlsFingerprintProfilesApiFixture = vi.hoisted(() => ({
      list: vi.fn().mockResolvedValue([])
    }))



vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

import EditAccountModal from '../EditAccountModal.vue'
import ExcelBPSAuthorization from '../ExcelBPSAuthorization.vue'
import CloudGatewaySettings from '../CloudGatewaySettings.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    },
    emptyMode: String
  },
  emits: ['update:modelValue'],
  template: `
    <div>
      <button
        type="button"
        data-testid="rewrite-to-snapshot"
        @click="$emit('update:modelValue', ['gpt-5.2-2025-12-11'])"
      >
        rewrite
      </button>
      <span data-testid="model-whitelist-value">
        {{ Array.isArray(modelValue) ? modelValue.join(',') : '' }}
      </span>
    </div>
  `
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: ''
    },
    options: {
      type: Array,
      default: () => []
    }
  },
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
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="group-selector">
      <button
        type="button"
        data-testid="set-shadow-group"
        @click="$emit('update:modelValue', [7])"
      >
        group
      </button>
    </div>
  `
})

function buildAccount() {
  return {
    id: 1,
    name: 'OpenAI Key',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: {
      api_key: 'sk-test',
      base_url: 'https://api.openai.com',
      model_mapping: {
        'gpt-5.2': 'gpt-5.2'
      }
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildOpenAISparkShadowAccount() {
  const account = buildAccount()
  return {
    ...account,
    id: 4,
    name: 'OpenAI Spark Shadow',
    type: 'oauth',
    parent_account_id: 1,
    credentials: {
      access_token: 'parent-access-token',
      refresh_token: 'parent-refresh-token',
      api_key: 'sk-parent',
      base_url: 'https://api.openai.com',
      model_allowlist: ['gpt-5.3-codex-spark'],
      compact_model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact'
      }
    },
    group_ids: []
  } as any
}

function buildVertexAccount() {
  return {
    id: 2,
    name: 'Vertex SA',
    notes: '',
    platform: 'gemini',
    type: 'service_account',
    credentials: {
      service_account_json: '{"type":"service_account","client_email":"sa@example.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\\nMIIE\\n-----END PRIVATE KEY-----\\n"}',
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildAntigravityAccount(projectId = 'configured-project') {
  return {
    id: 3,
    name: 'Antigravity OAuth',
    notes: '',
    platform: 'antigravity',
    type: 'oauth',
    credentials: {
      antigravity_project_id: projectId,
      model_mapping: {
        'gemini-2.5-flash': 'gemini-2.5-flash'
      }
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildGrokOAuthAccount() {
  return {
    id: 5,
    name: 'Grok OAuth',
    notes: '',
    platform: 'grok',
    type: 'oauth',
    credentials: {
      refresh_token: 'grok-rt',
      base_url: 'https://api.x.ai/v1',
      model_mapping: {
        'grok-latest': 'grok-4.3'
      }
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildGrokAPIKeyAccount() {
  return {
    ...buildAccount(),
    id: 6,
    name: 'Grok API Key',
    platform: 'grok',
    credentials: {},
    credentials_status: { has_api_key: true },
    concurrency: 2
  } as any
}

function buildOpenAISetupTokenAccount() {
  return {
    ...buildAccount(),
    type: 'setup-token',
    extra: {
      openai_oauth_responses_websockets_v2_mode: 'ctx_pool',
      openai_oauth_responses_websockets_v2_enabled: true
    }
  } as any
}

function buildOpenAIOAuthParentAccount() {
  return {
    ...buildAccount(),
    id: 7,
    name: 'OpenAI OAuth Parent',
    type: 'oauth',
    parent_account_id: null,
    credentials: { access_token: 'oauth-token' },
    extra: {}
  } as any
}

function mountModal(account = buildAccount(), renderGroupSelector = false) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: []
    },
    global: {
      provide: { [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } },
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: true,
        ExcelBPSAuthorization: true,
        CloudGatewaySettings: true,
        GroupSelector: renderGroupSelector ? false : GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub
      }
    }
  })
}

describe('EditAccountModal', () => {
  it('discards a completed save after switching the edited resource', async () => {
    const account = buildAccount()
    let finish: (value: typeof account) => void = () => { throw new Error('Save was not started') }
    updateAccountMock.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ account: { ...account, id: 99, name: 'New target' } })
    finish({ ...account, name: 'Old target saved' })
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect((wrapper.get('form input[type="text"]').element as HTMLInputElement).value).toBe('New target')
    wrapper.unmount()
  })

  it('does not emit success when a save completes after closing and reopening', async () => {
    const account = buildAccount()
    let finish: (value: typeof account) => void = () => { throw new Error('Save was not started') }
    updateAccountMock.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    finish(account)
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    { raw: undefined, expected: [] }, { raw: null, expected: [] },
    { raw: 'gpt-model', expected: [] }, { raw: { model: 'gpt-model' }, expected: [] },
    { raw: [], expected: [] }, { raw: [42, false, null, {}, '', '  '], expected: [] },
    { raw: [' gpt-model ', 42, 'gpt-model', 'image-model'], expected: ['gpt-model', 'image-model'] }
  ])('hydrates only explicit BPS selections and clears the previous account: $raw', async ({ raw, expected }) => {
    const first = buildOpenAIOAuthParentAccount()
    first.extra = { openai_excel_bps: true, openai_excel_bps_models: ['previous-account-model'] }
    const wrapper = mountModal(first)
    try {
      const account = { ...first, id: 88, extra: { openai_excel_bps: true, openai_excel_bps_models: raw } }
      if (raw === undefined) delete account.extra.openai_excel_bps_models
      const saveAccount = vi.fn().mockResolvedValue(account)
      await wrapper.setProps({ account, saveAccount })
      await flushPromises()
      expect(wrapper.find('[data-testid="excel-bps-all-models"]').exists()).toBe(false)
      expect(wrapper.get('[data-testid="excel-bps-model-selection"] [data-testid="model-whitelist-value"]').text()).toBe(expected.join(','))
      expect(wrapper.get('[data-testid="excel-bps-model-selection"]').getComponent(ModelWhitelistSelectorStub).props('emptyMode')).toBe('explicit')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0]?.[1].extra.openai_excel_bps_models).toEqual(expected)
    } finally { wrapper.unmount() }
  })

  it.each([true, false])('saves Cloud=%s without changing the existing BPS switch, model rules or native credentials', async (enabled) => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'native-token', refresh_token: 'native-refresh' }
    account.extra = {
      openai_cloud: !enabled,
      openai_excel_bps: true,
      openai_excel_bps_models: ['gpt-model'],
      openai_excel_bps_cache_creation_as_input: true,
      openai_excel_bps_omit_unsupported_tools: true
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const cloud = wrapper.findComponent(CloudGatewaySettings)
    // Saving Cloud needs no separate authorization/status response.
    cloud.vm.$emit('update:enabled', enabled)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const saved = updateAccountMock.mock.calls.at(-1)?.[1]
    expect(saved.extra.openai_cloud).toBe(enabled)
    expect(saved.extra).not.toHaveProperty('openai_excel_bps')
    expect(account.extra.openai_excel_bps).toBe(true)
    expect(saved.extra).toMatchObject({
      openai_excel_bps_models: ['gpt-model'],
      openai_excel_bps_cache_creation_as_input: true,
      openai_excel_bps_omit_unsupported_tools: true
    })
    expect(saved.credentials).toMatchObject({ access_token: 'native-token', refresh_token: 'native-refresh' })
    expect(wrapper.find('[data-testid="openai-protocol-selection"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cloud-test"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="excel-bps-toggle"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it.each([true, false].flatMap(cloud => [
    { raw: undefined, expected: [] }, { raw: [], expected: [] },
    { raw: 'invalid', expected: [] }, { raw: ['mapped-model'], expected: ['mapped-model'] }
  ].map(selection => ({ cloud, ...selection }))))('keeps explicit BPS selection across Cloud=$cloud: $raw', async ({ cloud, raw, expected }) => {
    const account = buildOpenAIOAuthParentAccount()
    account.credentials = { access_token: 'native-token', model_mapping: { alias: 'mapped-model' } }
    account.extra = { openai_cloud: !cloud, openai_excel_bps: true, openai_excel_bps_models: raw }
    if (raw === undefined) delete account.extra.openai_excel_bps_models
    const saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      wrapper.getComponent(CloudGatewaySettings).vm.$emit('update:enabled', cloud)
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0]?.[1].extra).toMatchObject({ openai_cloud: cloud, openai_excel_bps_models: expected })
      expect(saveAccount.mock.calls[0]?.[1].extra).not.toHaveProperty('openai_excel_bps')
      expect(account.extra.openai_excel_bps).toBe(true)
      expect(saveAccount.mock.calls[0]?.[1].credentials).toMatchObject({ access_token: 'native-token' })
      // BPS selections name final upstream models; saving must retain the separate alias mapping.
      expect(saveAccount.mock.calls[0]?.[1].credentials.model_mapping).toEqual({ alias: 'mapped-model' })
      expect(saveAccount.mock.calls[0]?.[1].extra.openai_excel_bps_models).not.toContain('alias')
    } finally { wrapper.unmount() }
  })

  it.each([true, false])('uses one account toggle without refilling settings or retaining old modes (enabled=%s)', async enabled => {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = { openai_excel_bps: enabled, openai_excel_bps_config_mode: 'defaults',
      openai_excel_bps_models: ['selected-model'], openai_excel_bps_cache_creation_as_input: true,
      openai_excel_bps_omit_unsupported_tools: true, openai_excel_bps_ignore_encrypted_content: true }
    const saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      expect(wrapper.find('[data-testid="excel-bps-modes"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="excel-bps-defaults-toggle"]').exists()).toBe(false)
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      if (enabled) await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      expect(wrapper.get('[data-testid="excel-bps-model-selection"] [data-testid="model-whitelist-value"]').text()).toBe('selected-model')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      const saved = saveAccount.mock.calls[0]?.[1].extra
      expect(saved).not.toHaveProperty('openai_excel_bps_config_mode')
      expect(saved).toMatchObject({ openai_excel_bps: true, openai_excel_bps_models: ['selected-model'],
        openai_excel_bps_cache_creation_as_input: true, openai_excel_bps_omit_unsupported_tools: true,
        openai_excel_bps_ignore_encrypted_content: true })
    } finally { wrapper.unmount() }
  })

  it('sends explicit false only when the single account toggle is disabled and saved', async () => {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = { openai_excel_bps: true, openai_excel_bps_config_mode: 'initial', openai_excel_bps_models: ['selected-model'] }
    const saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      expect(saveAccount).not.toHaveBeenCalled()
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0]?.[1].extra).toHaveProperty('openai_excel_bps', false)
      expect(saveAccount.mock.calls[0]?.[1].extra).not.toHaveProperty('openai_excel_bps_config_mode')
    } finally { wrapper.unmount() }
  })

  it('does not seed historical missing selections when the single toggle is enabled', async () => {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = { openai_excel_bps: false }
    const wrapper = mountModal(account)
    try {
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      expect(wrapper.get('[data-testid="excel-bps-model-selection"] [data-testid="model-whitelist-value"]').text()).toBe('')
    } finally { wrapper.unmount() }
  })

  function systemDisabledAccount() {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = {
      openai_excel_bps: false, openai_excel_bps_config_mode: 'initial', openai_excel_bps_models: ['gpt-model'],
      openai_excel_bps_auto_disable_on_403: true, openai_excel_bps_auto_recover_on_403: true,
      openai_excel_bps_403_recovery_interval_minutes: 120
    }
    account.excel_bps_403_recovery = { state: 'pending', interval_minutes: 120, next_probe_at: '2099-01-01T00:00:00Z' }
    return account
  }

  it('binds an explicit BPS re-enable to the exact safe disabled timestamp', async () => {
    const account = systemDisabledAccount()
    const disabledAt = '2026-10-27T01:02:03.000004+00:00'
    account.excel_bps_403_recovery = { state: 'pending', disabled_at: disabledAt, interval_minutes: 120 }
    const saveAccount = vi.fn().mockResolvedValue(account), wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][1].extra).toMatchObject({
        openai_excel_bps: true, openai_excel_bps_expected_disabled_at: disabledAt,
        openai_excel_bps_models: ['gpt-model']
      })
      expect(account.extra).not.toHaveProperty('openai_excel_bps_expected_disabled_at')
    } finally { wrapper.unmount() }
  })

  it('omits an untouched enabled switch and never echoes a stored request condition', async () => {
    const account = systemDisabledAccount()
    account.extra = { ...account.extra, openai_excel_bps: true, openai_excel_bps_expected_disabled_at: 'untrusted-old-condition' }
    const saveAccount = vi.fn().mockResolvedValue(account), wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('form input[type="text"]').setValue('Only rename')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps')
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps_expected_disabled_at')
    } finally { wrapper.unmount() }
  })

  it('keeps the re-enable condition captured by the user action instead of adopting a newer shutdown', async () => {
    const account = systemDisabledAccount()
    const initial = '2026-10-27T01:02:03.000004Z'
    account.excel_bps_403_recovery = { state: 'pending', disabled_at: initial, interval_minutes: 120 }
    const saveAccount = vi.fn().mockResolvedValue(account), wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      account.excel_bps_403_recovery.disabled_at = '2026-10-27T02:02:03.000004Z'
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount.mock.calls[0][1].extra.openai_excel_bps_expected_disabled_at).toBe(initial)
    } finally { wrapper.unmount() }
  })

  it.each(['target', 'close', 'permission'] as const)('clears an unsaved re-enable condition after %s changes', async change => {
    const account = systemDisabledAccount()
    account.excel_bps_403_recovery = { state: 'pending', disabled_at: '2026-10-27T01:00:00Z', interval_minutes: 120 }
    const saveAccount = vi.fn().mockResolvedValue(account), wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      if (change === 'target') await wrapper.setProps({ account: { ...systemDisabledAccount(), id: 99 } })
      if (change === 'close') { await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }) }
      if (change === 'permission') { await wrapper.setProps({ allowTransportEdit: false }); await wrapper.setProps({ allowTransportEdit: true }) }
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps')
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps_expected_disabled_at')
    } finally { wrapper.unmount() }
  })

  it.each(['enable', 'stop'] as const)('does not carry a BPS %s intent into a replacement save port before first submit', async action => {
    const account = systemDisabledAccount()
    account.excel_bps_403_recovery = { state: 'pending', disabled_at: '2026-10-27T01:00:00.000004Z', interval_minutes: 120 }
    const originalWriter = vi.fn().mockResolvedValue(account), replacementWriter = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount: originalWriter })
      await wrapper.get(action === 'enable' ? '[data-testid="excel-bps-toggle"]' : '[data-testid="excel-bps-stop-recovery"]').trigger('click')
      await wrapper.setProps({ saveAccount: replacementWriter })
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(originalWriter).not.toHaveBeenCalled()
      expect(replacementWriter).toHaveBeenCalledTimes(1)
      expect(replacementWriter.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps')
      expect(replacementWriter.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps_expected_disabled_at')
    } finally { wrapper.unmount() }
  })

  it('keeps a rejected BPS draft open and requires reloading instead of replaying a 409', async () => {
    const account = systemDisabledAccount()
    account.excel_bps_403_recovery = { state: 'pending', disabled_at: '2026-10-27T01:02:03.000004Z', interval_minutes: 120 }
    const saveAccount = vi.fn().mockRejectedValue({ status: 409, reason: 'OPENAI_EXCEL_BPS_STATE_CONFLICT', message: 'state changed' })
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(wrapper.get('[data-testid="bps-state-conflict"]').text()).toContain('excelBPSStateConflict')
      expect(wrapper.emitted('close')).toBeUndefined()
      expect(wrapper.emitted('updated')).toBeUndefined()
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
    } finally { wrapper.unmount() }
  })

  it('does not infer an actionable recovery state from a private record without the safe projection', () => {
    const account = systemDisabledAccount()
    delete account.excel_bps_403_recovery
    account.extra = { ...account.extra, openai_excel_bps_failure: { source: 'system', state: 'disabled' } }
    const wrapper = mountModal(account)
    try { expect(wrapper.find('[data-testid="excel-bps-stop-recovery"]').exists()).toBe(false) }
    finally { wrapper.unmount() }
  })

  it('sends explicit false only after the administrator chooses stop and saves', async () => {
    const account = systemDisabledAccount(), saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-stop-recovery"]').trigger('click')
      expect(saveAccount).not.toHaveBeenCalled()
      expect(wrapper.text()).toContain('excelBPSStopRecoveryPending')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][1].extra).toMatchObject({
        openai_excel_bps: false, openai_excel_bps_models: ['gpt-model'],
        openai_excel_bps_auto_recover_on_403: true
      })
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps_failure')
    } finally { wrapper.unmount() }
  })

  it('does not turn an unrelated edit of a disabled account into a stop command', async () => {
    const account = systemDisabledAccount(), saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('form input[type="text"]').setValue('Only rename')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps')
    } finally { wrapper.unmount() }
  })

  it.each(['target', 'close', 'permission'] as const)('clears an unsaved stop intent on %s change', async change => {
    const account = systemDisabledAccount(), saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-stop-recovery"]').trigger('click')
      if (change === 'target') await wrapper.setProps({ account: { ...systemDisabledAccount(), id: 99 } })
      if (change === 'close') { await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }) }
      if (change === 'permission') { await wrapper.setProps({ allowTransportEdit: false }); await wrapper.setProps({ allowTransportEdit: true }) }
      await flushPromises()
      expect(wrapper.text()).not.toContain('excelBPSStopRecoveryPending')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(saveAccount.mock.calls[0][0]).toBe(change === 'target' ? 99 : account.id)
      expect(saveAccount.mock.calls[0][1].extra).not.toHaveProperty('openai_excel_bps')
    } finally { wrapper.unmount() }
  })

  it.each([{ allowTransportEdit: false }, { supplierScope: true }, { captureBaseline: true }])('does not expose the stop action across the existing boundary %j', async overrides => {
    const wrapper = mountModal(systemDisabledAccount())
    try { await wrapper.setProps(overrides); expect(wrapper.find('[data-testid="excel-bps-stop-recovery"]').exists()).toBe(false) }
    finally { wrapper.unmount() }
  })

  it('an intentional enabled-to-disabled toggle also submits explicit false', async () => {
    const account = systemDisabledAccount()
    account.extra = { ...account.extra, openai_excel_bps: true }
    const saveAccount = vi.fn().mockResolvedValue(account), wrapper = mountModal(account)
    try {
      await wrapper.setProps({ saveAccount })
      await wrapper.get('[data-testid="excel-bps-toggle"]').trigger('click')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
      expect(saveAccount.mock.calls[0][1].extra.openai_excel_bps).toBe(false)
    } finally { wrapper.unmount() }
  })

  it.each([
    { state: 'reauthorization_required', reason: 'invalid_grant', interval_minutes: 120 },
    { state: 'future-state', reason: 'future_reason', interval_minutes: 120 },
    { state: 'unknown', interval_minutes: 120 }
  ])('preserves BPS configuration on an unrelated edit with system/unknown state %j', failure => {
    const account = buildOpenAIOAuthParentAccount()
    const saved = {
      openai_excel_bps_models: ['gpt-model'],
      openai_excel_bps_cache_creation_as_input: true, openai_excel_bps_omit_unsupported_tools: false,
      openai_excel_bps_ignore_encrypted_content: true
    }
    account.extra = { ...saved, openai_excel_bps: false }
    account.excel_bps_403_recovery = failure
    const saveAccount = vi.fn().mockResolvedValue(account)
    const wrapper = mountModal(account)
    return (async () => {
      try {
        await wrapper.setProps({ saveAccount })
        await wrapper.get('form input[type="text"]').setValue('Unrelated rename')
        await wrapper.get('form#edit-account-form').trigger('submit.prevent')
        await flushPromises()
        expect(saveAccount).toHaveBeenCalledTimes(1)
        const payload = saveAccount.mock.calls[0][1]
        expect(payload.extra).toMatchObject(saved)
        expect(payload.extra).not.toHaveProperty('openai_excel_bps_failure')
        expect(payload.extra.openai_excel_bps).not.toBe(true)
        expect(account.excel_bps_403_recovery).toBe(failure)
        expect(payload).not.toHaveProperty('excel_bps_403_recovery')
      } finally { wrapper.unmount() }
    })()
  })

  it.each([
    { enabled: true, combined: false },
    { enabled: true, combined: true },
    { enabled: false, combined: false },
    { enabled: false, combined: true }
  ])('omits server-owned BPS runtime from the injected writer (enabled=$enabled, combined=$combined)', async ({ enabled, combined }) => {
    const account = buildOpenAIOAuthParentAccount()
    const runtime = Object.freeze({
      openai_excel_bps_failure: Object.freeze({
        id: 'server-failure',
        grant_fingerprint: 'server-grant',
        policy: Object.freeze({})
      }),
      openai_excel_bps_403_disabled_at: '2026-10-27T00:00:00Z',
      openai_excel_bps_403_last_probe_at: '2026-10-27T01:00:00Z',
      openai_excel_bps_403_last_probe_result: 'server-result',
      openai_excel_bps_403_moved_at: '2026-10-27T00:00:00Z',
      openai_excel_bps_403_moved_group_id: 0
    })
    const editable = Object.freeze({
      openai_excel_bps_models: Object.freeze(['gpt-model']),
      openai_excel_bps_auto_disable_on_403: true,
      openai_excel_bps_auto_recover_on_403: true,
      openai_excel_bps_403_recovery_interval_minutes: 120,
      openai_excel_bps_auto_move_on_403: true,
      openai_excel_bps_403_target_group_id: 0,
      openai_long_context_billing_enabled: false,
      editable_extension: Object.freeze({ enabled: false, limit: 0, label: '', selection: Object.freeze([]), cleared: null }),
      extension_false: false,
      extension_zero: 0,
      extension_empty: '',
      extension_null: null,
      openai_excel_bps_extension: Object.freeze({ enabled: false })
    })
    const originalExtra = Object.freeze({ ...runtime, ...editable, openai_cloud: true, openai_excel_bps: enabled })
    account.extra = originalExtra
    const saveAccount = vi.fn().mockResolvedValue(account)
    updateAccountMock.mockReset()
    const wrapper = mountModal(account)
    await wrapper.setProps({ saveAccount })
    try {
      await wrapper.get('form input[type="text"]').setValue('Renamed BPS account')
      if (combined) {
        wrapper.findComponent(CloudGatewaySettings).vm.$emit('update:enabled', false)
        await flushPromises()
      }
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()
      expect(saveAccount).toHaveBeenCalledTimes(1)
      expect(updateAccountMock).not.toHaveBeenCalled()
      const [accountID, saved] = saveAccount.mock.calls[0]
      expect(accountID).toBe(account.id)
      expect(saved.name).toBe('Renamed BPS account')
      expect(saved.extra).not.toBe(originalExtra)
      expect(saved.extra).toMatchObject({ ...editable, openai_cloud: !combined })
      expect(saved.credentials).toMatchObject(account.credentials)
      expect(account.extra).toBe(originalExtra)
      expect(account.extra).toEqual({ ...runtime, ...editable, openai_cloud: true, openai_excel_bps: enabled })
      for (const key of Object.keys(runtime)) expect(saved.extra).not.toHaveProperty(key)
    } finally {
      wrapper.unmount()
    }
  })

  it.each([true, false, undefined])('preserves hidden BPS options (%s) when saving a 403-disabled account', async (option) => {
    const account = buildOpenAIOAuthParentAccount()
    const hiddenKeys = ['openai_excel_bps_cache_creation_as_input', 'openai_excel_bps_omit_unsupported_tools', 'openai_excel_bps_ignore_encrypted_content']
    const retained: Record<string, unknown> = {
      openai_excel_bps_models: ['gpt-model'],
      openai_excel_bps_auto_disable_on_403: true,
      openai_excel_bps_auto_recover_on_403: true,
      openai_excel_bps_403_recovery_interval_minutes: 120
    }
    if (option !== undefined) for (const key of hiddenKeys) retained[key] = option
    account.extra = { ...retained, openai_excel_bps: false, openai_excel_bps_403_disabled_at: '2026-10-27T00:00:00Z' }
    account.excel_bps_403_recovery = { state: 'pending', interval_minutes: 120 }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    expect(wrapper.get('[data-testid="excel-bps-toggle"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="excel-bps-model-selection"]').exists()).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="excel-bps-auto-disable-on-403"]').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="excel-bps-auto-recover-on-403"]').element.checked).toBe(true)
    await wrapper.get('form input[type="text"]').setValue('Renamed while BPS is disabled')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const saved = updateAccountMock.mock.calls[0][1]
    expect(saved.extra).toMatchObject(retained)
    expect(saved.extra).not.toHaveProperty('openai_excel_bps_403_disabled_at')
    expect(saved.extra.openai_excel_bps).not.toBe(true)
    for (const key of hiddenKeys) {
      expect(Object.prototype.hasOwnProperty.call(saved.extra, key)).toBe(option !== undefined)
    }
    wrapper.unmount()
  })

  it('can stop recovery and change its interval while BPS remains disabled', async () => {
    const account = buildOpenAIOAuthParentAccount()
    account.excel_bps_403_recovery = { state: 'pending', interval_minutes: 60 }
    account.extra = {
      openai_excel_bps: false,
      openai_excel_bps_403_disabled_at: '2026-10-27T00:00:00Z',
      openai_excel_bps_auto_disable_on_403: true,
      openai_excel_bps_auto_recover_on_403: true,
      openai_excel_bps_403_recovery_interval_minutes: 60
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="excel-bps-recovery-interval"]').setValue(120)
    await wrapper.get('[data-testid="excel-bps-auto-recover-on-403"]').setValue(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const saved = updateAccountMock.mock.calls[0][1]
    expect(saved.extra).toMatchObject({
      openai_excel_bps_auto_disable_on_403: true,
      openai_excel_bps_403_recovery_interval_minutes: 120
    })
    expect(saved.extra).not.toHaveProperty('openai_excel_bps_403_disabled_at')
    expect(saved.extra.openai_excel_bps).not.toBe(true)
    expect(saved.extra.openai_excel_bps_auto_recover_on_403).not.toBe(true)
    wrapper.unmount()
  })

  it('preserves native OAuth and BPS settings when saving account edits', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'codex-token', refresh_token: 'codex-refresh' }
    account.extra = { openai_excel_bps: true, openai_excel_bps_models: ['gpt-model'],
      openai_oauth_responses_websockets_v2_mode: 'passthrough', openai_oauth_responses_websockets_v2_enabled: true }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const saved = updateAccountMock.mock.calls.at(-1)?.[1]
    expect(saved.extra).not.toHaveProperty('openai_excel_bps')
    expect(account.extra.openai_excel_bps).toBe(true)
    expect(saved.extra).toMatchObject({ openai_excel_bps_models: ['gpt-model'],
      openai_oauth_responses_websockets_v2_mode: 'passthrough' })
    expect(saved.credentials).toMatchObject({ access_token: 'codex-token', refresh_token: 'codex-refresh' })
    wrapper.unmount()
  })

  it.each(['oauth', 'setup-token'])('loads and saves Anthropic %s restrictions while preserving credentials and mappings', async (type) => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.type = type
    account.credentials = {
      access_token: 'keep-token', refresh_token: 'keep-refresh',
      model_mapping: { 'claude-sonnet-5-5': 'claude-sonnet-5-5', 'claude-alias': 'claude-opus-5-5' }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const selector = wrapper.findComponent(ModelWhitelistSelectorStub)
    expect(selector.exists()).toBe(true)
    expect(selector.attributes('account-id')).toBe('1')
    expect(selector.props('modelValue')).toEqual(['claude-sonnet-5-5'])
    selector.vm.$emit('update:modelValue', ['claude-haiku-5-5'])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      access_token: 'keep-token', refresh_token: 'keep-refresh',
      model_allowlist: ['claude-haiku-5-5'],
      model_mapping: { 'claude-alias': 'claude-opus-5-5' }
    })
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials.model_mapping).not.toHaveProperty('claude-sonnet-5-5')
    wrapper.unmount()
  })

  it.each(['oauth', 'setup-token'])('clears Anthropic %s whitelist without dropping its token', async (type) => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.type = type
    account.credentials = { access_token: 'keep-token', model_mapping: { 'claude-haiku-5-5': 'claude-haiku-5-5' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    wrapper.findComponent(ModelWhitelistSelectorStub).vm.$emit('update:modelValue', [])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials.access_token).toBe('keep-token')
    expect(credentials).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })

  it('edits and removes the Fable override independently from the account threshold', async () => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.type = 'oauth'
    account.credentials = { access_token: 'token', account_scheduling_threshold: 70, anthropic_fable_scheduling_threshold: 95 }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const enabled = wrapper.get('[data-testid="anthropic-fable-scheduling-threshold-override-enabled"]')
    expect((enabled.element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-testid="anthropic-fable-scheduling-threshold-override-value"]').setValue(90)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls.at(-1)?.[1]?.credentials).toEqual(expect.objectContaining({ anthropic_fable_scheduling_threshold: 90 }))
    expect(updateAccountMock.mock.calls.at(-1)?.[1]?.credentials?.account_scheduling_threshold).toBe(70)
    await enabled.setValue(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls.at(-1)?.[1]?.credentials).toEqual(expect.objectContaining({ anthropic_fable_scheduling_threshold: null }))
    wrapper.unmount()
  })


  beforeEach(() => {
    authIsSimpleMode.value = true
    authCanMock.mockReset().mockReturnValue(true)
    authFieldMock.mockReset().mockReturnValue(true)
  })

  afterEach(() => vi.useRealTimers())

  it.each([true, false])('renders Excel authorization according to accounts.authorize permission (%s)', (allowed) => {
    authCanMock.mockReturnValue(allowed)
    const account = buildOpenAIOAuthParentAccount()
    account.proxy_id = 23
    const wrapper = mountModal(account)
    const authorization = wrapper.findComponent(ExcelBPSAuthorization)

    expect(authCanMock).toHaveBeenCalledWith('accounts.authorize')
    expect(authorization.exists()).toBe(allowed)
    if (allowed) {
      expect(authorization.props()).toEqual({ accountId: account.id, proxyId: 23 })
    }
    wrapper.unmount()
  })

  it.each(['accounts.authorize', 'credentials field'])('does not let accounts.manage bypass %s for credential-form references', async boundary => {
    if (boundary === 'accounts.authorize') authCanMock.mockImplementation(permission => permission !== 'accounts.authorize')
    else authFieldMock.mockImplementation((_schema, field) => field !== 'credentials')
    const account = buildAccount()
    account.credentials = { ...account.credentials, model_allowlist: ['seed'], model_mapping: { alias: 'upstream' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    const wrapper = mountModal(account)
    await flushPromises()
    wrapper.getComponent(ModelWhitelistSelectorStub).vm.$emit('update:modelValue', ['unauthorized-draft'])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({ model_allowlist: ['seed'], model_mapping: { alias: 'upstream' } })
    wrapper.unmount()
  })

  it('preserves saved model fields while ignoring stale drafts without accounts.manage permission', async () => {
    authCanMock.mockImplementation(permission => permission !== 'accounts.manage')
    const account = buildAccount()
    account.credentials = { ...account.credentials, model_allowlist: ['seed'], model_mapping: { legacy: 'legacy', alias: 'upstream' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    const wrapper = mountModal(account)
    await flushPromises()
    wrapper.getComponent(ModelWhitelistSelectorStub).vm.$emit('update:modelValue', ['unauthorized-draft'])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({ model_allowlist: ['seed'], model_mapping: { legacy: 'legacy', alias: 'upstream' } })
    wrapper.unmount()
  })

  it('saves independent model references atomically when transport editing is unavailable', async () => {
    const account = buildAccount()
    account.credentials = { ...account.credentials, model_allowlist: ['seed'], model_mapping: { alias: 'upstream' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    const wrapper = mountModal(account)
    await flushPromises()
    wrapper.getComponent(ModelWhitelistSelectorStub).vm.$emit('update:modelValue', ['seed', 'new'])
    await wrapper.setProps({ allowTransportEdit: false })
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({ model_allowlist: ['seed', 'new'], model_mapping: { alias: 'upstream' } })
    wrapper.unmount()
  })

  it('keeps existing real rewrites independent of the whitelist and preserves them on save', async () => {
    const account = buildAccount()
    account.credentials.model_mapping = { 'gpt-5.2': 'gpt-5.2', 'gpt-latest': 'deepseek-chat' }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['gpt-5.2'])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({ 'gpt-latest': 'deepseek-chat' })
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_allowlist).toEqual(['gpt-5.2'])
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, account: { ...account } })
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['gpt-5.2'])
  })

  it('sets expiry presets from now instead of extending the saved expiry', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2028-02-29T12:34:00'))
    const account = buildAccount()
    account.expires_at = new Date('2030-06-15T09:00:00').getTime() / 1000
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')

    for (const [label, expected] of [
      ['payment.oneMonth', '2028-03-29T12:34'],
      ['payment.oneYear', '2029-02-28T12:34'],
    ]) {
      const button = wrapper.findAll('button').find((candidate) => candidate.text() === label)!
      expect(button.attributes('type')).toBe('button')
      await button.trigger('click')
      expect(input.element.value).toBe(expected)
      expect(updateAccountMock).not.toHaveBeenCalled()
    }

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.expires_at).toBe(new Date('2029-02-28T12:34:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('can clear a selected expiry preset before saving the account', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const button = wrapper.findAll('button').find((candidate) => candidate.text() === 'payment.oneYear')!
    await button.trigger('click')
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')
    expect(input.element.value).not.toBe('')
    await input.setValue('')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.expires_at).toBe(0)
    wrapper.unmount()
  })

  it('allows removing assigned inactive groups and undoing the selection before saving', async () => {
    authIsSimpleMode.value = false
    const account = buildAccount()
    const activeGroup = {
      id: 1,
      name: 'Active group',
      platform: 'openai',
      status: 'active',
      subscription_type: 'standard',
      rate_multiplier: 1
    }
    const inactiveGroup = { ...activeGroup, id: 2, name: 'Paused group', status: 'inactive' }
    account.group_ids = [1, 2]
    account.groups = [
      { ...activeGroup, name: 'Outdated name' },
      inactiveGroup,
      inactiveGroup,
      { ...inactiveGroup, id: 3, name: 'Unassigned paused group' }
    ]
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, true)
    await wrapper.setProps({ groups: [activeGroup] as any })
    const selector = wrapper.get('[data-tour="account-form-groups"]')
    expect(selector.findAll('input[type="checkbox"]').map(input => input.attributes('value')))
      .toEqual(['1', '2'])
    expect(selector.text()).toContain('Active group')
    expect(selector.text()).not.toContain('Outdated name')
    const pausedCheckbox = selector.get<HTMLInputElement>('input[value="2"]')
    expect(pausedCheckbox.element.checked).toBe(true)

    await pausedCheckbox.setValue(false)
    expect(selector.get<HTMLInputElement>('input[value="2"]').element.checked).toBe(false)
    await pausedCheckbox.setValue(true)
    expect(pausedCheckbox.element.checked).toBe(true)
    await pausedCheckbox.setValue(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.group_ids).toEqual([1])
    expect(account.group_ids).toEqual([1, 2])
  })

  it('reopening the same account rehydrates the OpenAI whitelist from props', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2-2025-12-11')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_allowlist).toEqual(['gpt-5.2'])
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
  })

  it('preserves OpenCode Zen account type and endpoints on submit', async () => {
    const account = buildAccount()
    account.platform = 'opencode_go'
    account.credentials = {
      api_key: 'sk-opencode',
      account_mode: 'zen',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/v1',
        anthropic: 'https://opencode.ai/zen',
        responses: 'https://opencode.ai/zen/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'claude-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'zen',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/v1',
        anthropic: 'https://opencode.ai/zen',
        responses: 'https://opencode.ai/zen/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'claude-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    })
  })

  it('treats a legacy OpenCode account without account_mode as GO', async () => {
    const account = buildAccount()
    account.platform = 'opencode_go'
    account.credentials = {
      api_key: 'sk-opencode',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/go/v1',
        anthropic: 'https://opencode.ai/zen/go',
        responses: 'https://opencode.ai/zen/go/v1'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'go',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1'
    })
  })

  describe('providers using the generic form', () => {
    beforeEach(() => {
      setPlatformCatalog({
        platforms: [
          ...BUILTIN_PLATFORM_CATALOG.platforms,
          {
            id: 'acme_router',
            display_name: 'Acme Router',
            gateway: 'openai',
            cn_provider: false,
            multi_protocol: {
              default_mode: 'standard',
              routing: 'by_model',
              modes: [
                {
                  mode: 'standard',
                  base_urls: {
                    chat_completions: 'https://api.acme-router.example/provider/v1',
                    anthropic: 'https://api.acme-router.example/provider'
                  },
                  protocol_rules: [{ pattern: 'claude-*', protocol: 'anthropic' }]
                },
                {
                  mode: 'team',
                  base_urls: {
                    chat_completions: 'https://team.acme-router.example/provider/v1',
                    anthropic: 'https://team.acme-router.example/provider'
                  },
                  protocol_rules: [{ pattern: 'sonnet-*', protocol: 'anthropic' }]
                }
              ]
            }
          }
        ],
        composite_precedence: [...BUILTIN_PLATFORM_CATALOG.composite_precedence, 'acme_router']
      })
      checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    })

    afterEach(() => {
      resetPlatformCatalog()
    })

    function commandCodeAccount() {
      const account = buildAccount()
      account.platform = 'acme_router'
      account.credentials = {
        api_key: 'sk-cc',
        account_mode: 'standard',
        api_protocol: 'adaptive',
        base_url: 'https://relay.example.com/v1',
        api_base_urls: {
          chat_completions: 'https://relay.example.com/v1',
          anthropic: 'https://relay.example.com'
        },
        protocol_rules: [{ pattern: 'custom-*', protocol: 'anthropic' }]
      }
      updateAccountMock.mockReset().mockResolvedValue(account)
      return account
    }

    it('preserves stored endpoints and rules on submit', async () => {
      const wrapper = mountModal(commandCodeAccount())
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(updateAccountMock).toHaveBeenCalledTimes(1)
      expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
        account_mode: 'standard',
        api_protocol: 'adaptive',
        base_url: 'https://relay.example.com/v1',
        api_base_urls: {
          chat_completions: 'https://relay.example.com/v1',
          anthropic: 'https://relay.example.com'
        },
        protocol_rules: [{ pattern: 'custom-*', protocol: 'anthropic' }]
      })
      expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.api_base_urls).not.toHaveProperty('responses')
    })

    it('offers the provider modes and keeps customised endpoints when switching mode', async () => {
      const wrapper = mountModal(commandCodeAccount())
      const modeButtons = wrapper.get('[data-testid="edit-generic-account-mode"]').findAll('button')
      expect(modeButtons.map(button => button.text())).toEqual(['standard', 'team'])
      await modeButtons[1].trigger('click')
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
        account_mode: 'team',
        // 自定义端点与规则不是上一模式的默认值，切换模式时保留。
        api_base_urls: {
          chat_completions: 'https://relay.example.com/v1',
          anthropic: 'https://relay.example.com'
        },
        protocol_rules: [{ pattern: 'custom-*', protocol: 'anthropic' }]
      })
    })
  })

  it('preserves adaptive Kimi Responses endpoint on submit', async () => {
    const account = buildAccount()
    account.platform = 'kimi'
    account.credentials = {
      api_key: 'sk-kimi',
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.moonshot.cn/v1',
      api_base_urls: {
        chat_completions: 'https://api.moonshot.cn/v1',
        anthropic: 'https://api.moonshot.cn/anthropic',
        responses: 'https://api.moonshot.cn/v1'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.moonshot.cn/v1',
      api_base_urls: {
        chat_completions: 'https://api.moonshot.cn/v1',
        anthropic: 'https://api.moonshot.cn/anthropic',
        responses: 'https://api.moonshot.cn/v1'
      }
    })
  })

  it('preserves adaptive GLM endpoints on submit', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://open.bigmodel.cn/api/coding/paas/v4',
      api_base_urls: {
        chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
        anthropic: 'https://open.bigmodel.cn/api/anthropic'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://open.bigmodel.cn/api/coding/paas/v4',
      api_base_urls: {
        chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
        anthropic: 'https://open.bigmodel.cn/api/anthropic'
      }
    })
  })

  it.each([
    ['explicit Chat Completions', 'chat_completions'],
    ['legacy missing protocol', undefined]
  ])('preserves a custom CN relay for %s accounts', async (_name, storedProtocol) => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      base_url: 'https://relay.example.com/v1'
    }
    if (storedProtocol) {
      account.credentials.api_protocol = storedProtocol
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const submittedCredentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(submittedCredentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'chat_completions',
      base_url: 'https://relay.example.com/v1'
    })
    expect(submittedCredentials).not.toHaveProperty('api_base_urls')
  })

  it('uses the legacy base_url when adaptive endpoints are missing', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: '   '
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: 'https://relay.example.com/v1',
        anthropic: 'https://open.bigmodel.cn/api/anthropic'
      }
    })
  })

  it('carries a fixed Chat relay into Adaptive when the user switches protocols', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      api_protocol: 'chat_completions',
      base_url: 'https://relay.example.com/v1'
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    const adaptiveButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.accounts.cnProviders.apiProtocol.adaptive'))
    expect(adaptiveButton).toBeDefined()
    await adaptiveButton!.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: 'https://relay.example.com/v1'
      }
    })
  })

  it.each([
    {
      name: 'Anthropic',
      platform: 'zhipu',
      protocol: 'anthropic',
      baseUrl: 'https://relay.example.com/anthropic',
      expectedBaseUrl: 'https://open.bigmodel.cn/api/paas/v4',
      expectedProtocolUrls: {
        chat_completions: 'https://open.bigmodel.cn/api/paas/v4',
        anthropic: 'https://relay.example.com/anthropic'
      }
    },
    {
      name: 'Responses',
      platform: 'deepseek',
      protocol: 'responses',
      baseUrl: 'https://relay.example.com/responses',
      expectedBaseUrl: 'https://api.deepseek.com',
      expectedProtocolUrls: {
        chat_completions: 'https://api.deepseek.com',
        anthropic: 'https://api.deepseek.com/anthropic',
        responses: 'https://relay.example.com/responses'
      }
    }
  ])('keeps a fixed $name relay in its protocol slot when switching to Adaptive', async (testCase) => {
    const account = buildAccount()
    account.platform = testCase.platform
    account.credentials = {
      api_key: 'sk-cn',
      account_mode: 'payg',
      api_protocol: testCase.protocol,
      base_url: testCase.baseUrl
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    const adaptiveButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.accounts.cnProviders.apiProtocol.adaptive'))
    expect(adaptiveButton).toBeDefined()
    await adaptiveButton!.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: testCase.expectedBaseUrl,
      api_base_urls: testCase.expectedProtocolUrls
    })
  })

  it('preserves model mappings when editing the whitelist', async () => {
    const account = buildAccount()
    account.credentials.model_mapping = {
      'gpt-5.2': 'gpt-5.2',
      'gpt-latest': 'gpt-5.2'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_allowlist).toEqual(['gpt-5.2-2025-12-11'])
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({ 'gpt-latest': 'gpt-5.2' })
  })

  it('submits OpenAI compact mode and compact-only model mapping', async () => {
    const account = buildAccount()
    account.extra = {
      openai_compact_mode: 'force_on'
    }
    account.credentials = {
      ...account.credentials,
      compact_model_mapping: {
        'gpt-5.4': 'gpt-5.4-openai-compact'
      }
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_compact_mode).toBe('force_on')
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.compact_model_mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-openai-compact'
    })
  })

  it('loads and submits the per-account OpenAI long-context billing toggle', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('true')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('loads and clears the OAuth-only Codex namespace flatten toggle', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.extra = {
      openai_responses_flatten_namespaces: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="edit-openai-flatten-namespaces-toggle"]')

    // 关闭后应从 extra 中删除该键，而不是写入 false
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'openai_responses_flatten_namespaces'
    )
  })

  it('submits the Codex namespace flatten toggle when switched on', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="edit-openai-flatten-namespaces-toggle"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_flatten_namespaces).toBe(
      true
    )
  })

  it('writes the upstream request id header into extra only when it changes', async () => {
    const account = buildAccount()
    account.extra = { openai_compact_mode: 'force_on' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const untouched = mountModal(account)
    await untouched.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.upstream_request_id_header).toBeUndefined()

    updateAccountMock.mockClear()
    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue(' X-Oneapi-Request-Id ')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toMatchObject({
      openai_compact_mode: 'force_on',
      upstream_request_id_header: 'X-Oneapi-Request-Id'
    })
  })

  it('removes the upstream request id header from extra when cleared', async () => {
    const account = buildAccount()
    account.extra = { upstream_request_id_header: 'X-Request-ID' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    expect((wrapper.get('[data-testid="upstream-request-id-header"]').element as HTMLInputElement).value).toBe('X-Request-ID')
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toBeDefined()
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('upstream_request_id_header')
  })

  it('writes images_url_to_b64_json into extra when toggled on', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.images_url_to_b64_json).toBe(true)
  })

  it('removes images_url_to_b64_json from extra when toggled off', async () => {
    const account = buildAccount()
    account.extra = { images_url_to_b64_json: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toBeDefined()
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('images_url_to_b64_json')
  })

  it('hides the Codex namespace flatten toggle for non-OAuth OpenAI accounts', async () => {
    const account = buildAccount()
    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="edit-openai-flatten-namespaces-toggle"]').exists()).toBe(
      false
    )
  })

  it('defaults legacy OpenAI accounts to long-context billing disabled', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('does not render or submit the long-context billing toggle for Spark shadow accounts', async () => {
    const account = buildOpenAISparkShadowAccount()
    account.extra = {
      openai_long_context_billing_enabled: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'openai_long_context_billing_enabled'
    )
  })

  it('preserves an explicit OpenAI long-context billing opt-out', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('fails closed for malformed OpenAI long-context billing values', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: 'false'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="openai-long-context-billing-toggle"]').attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('loads and submits Grok OAuth model mapping edits', async () => {
    const account = buildGrokOAuthAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    expect(wrapper.text()).toContain('Imagine Image')
    expect(wrapper.text()).toContain('Imagine Video')

    const inputWithValue = (value: string) => {
      const input = wrapper
        .findAll('input')
        .find((input) => (input.element as HTMLInputElement).value === value)
      expect(input).toBeTruthy()
      return input!
    }

    await inputWithValue('grok-latest').setValue('grok')
    await inputWithValue('grok-4.3').setValue('grok-build-0.1')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      grok: 'grok-build-0.1'
    })
  })

  it('uses the official xAI base URL when a Grok API-key account omits base_url', async () => {
    const account = buildGrokAPIKeyAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect((wrapper.get('input[placeholder="https://api.x.ai/v1"]').element as HTMLInputElement).value)
      .toBe('https://api.x.ai/v1')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.base_url).toBe('https://api.x.ai/v1')
  })

  it('only submits model mapping credentials when saving an OpenAI spark shadow account', async () => {
    authIsSimpleMode.value = false
    const account = buildOpenAISparkShadowAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="set-shadow-group"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.group_ids).toEqual([7])
    expect(payload?.credentials).toEqual({
      model_allowlist: ['gpt-5.3-codex-spark'],
      compact_model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact'
      }
    })
  })

  it('submits OpenAI APIKey Responses support override mode', async () => {
    const account = buildAccount()
    account.extra = {
      openai_responses_mode: 'force_chat_completions',
      openai_responses_supported: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="openai-responses-mode-select"]').setValue('force_responses')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_mode).toBe('force_responses')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(false)
  })

  it('submits the account upstream billing auto-probe setting', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.upstream_billing_probe_enabled).toBe(true)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'upstream_billing_probe_enabled'
    )
  })

  it('exposes the upstream billing auto-probe toggle for non-OpenAI API-key accounts', async () => {
    // 探测已放宽到全部 API-key 平台：grok 账号同样能开启并保存。
    const account = buildAccount()
    account.platform = 'grok'
    account.name = 'grok-relay'
    account.credentials = { api_key: 'sk-grok', base_url: 'https://relay.example/v1' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('enabling rate sync also enables probing and stops submitting a manual rate', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const syncToggle = wrapper.get('[data-testid="upstream-billing-rate-sync"]')
    const probeToggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    const rateInput = wrapper.get<HTMLInputElement>('[data-testid="account-rate-multiplier"]')
    expect(syncToggle.attributes('aria-checked')).toBe('false')
    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(rateInput.element.disabled).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.billingRateMultiplierHint')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBilling.syncRateManagedHint')

    await syncToggle.trigger('click')
    expect(syncToggle.attributes('aria-checked')).toBe('true')
    expect(probeToggle.attributes('aria-checked')).toBe('true')
    expect(rateInput.element.disabled).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.syncRateManagedHint')
    expect(wrapper.text()).not.toContain('admin.accounts.billingRateMultiplierHint')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(true)
    expect(payload).not.toHaveProperty('rate_multiplier')
  })

  it('disabling probing also disables rate sync and restores manual rate editing', async () => {
    const account = buildAccount()
    account.extra = {
      upstream_billing_probe_enabled: true,
      upstream_billing_rate_sync_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const syncToggle = wrapper.get('[data-testid="upstream-billing-rate-sync"]')
    const probeToggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    const rateInput = wrapper.get<HTMLInputElement>('[data-testid="account-rate-multiplier"]')
    expect(syncToggle.attributes('aria-checked')).toBe('true')
    expect(rateInput.element.disabled).toBe(true)

    await probeToggle.trigger('click')
    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(syncToggle.attributes('aria-checked')).toBe('false')
    expect(rateInput.element.disabled).toBe(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(false)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(false)
    expect(payload?.rate_multiplier).toBe(1)
  })

  it('disabling only rate sync keeps automatic probing enabled', async () => {
    const account = buildAccount()
    account.extra = {
      upstream_billing_probe_enabled: true,
      upstream_billing_rate_sync_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="upstream-billing-rate-sync"]').trigger('click')
    expect(wrapper.get('[data-testid="upstream-billing-auto-probe"]').attributes('aria-checked')).toBe(
      'true'
    )
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(false)
    expect(payload?.rate_multiplier).toBe(1)
  })

  it('clears OpenAI APIKey Responses override when set back to auto', async () => {
    const account = buildAccount()
    account.extra = {
      openai_responses_mode: 'force_chat_completions',
      openai_responses_supported: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="openai-responses-mode-select"]').setValue('auto')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('openai_responses_mode')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(true)
  })

  it('submits OpenAI APIKey endpoint capabilities from credentials', async () => {
    const account = buildAccount()
    account.credentials.openai_capabilities = ['chat_completions']
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.findAll('input[type="checkbox"]').some((input) => (input.element as HTMLInputElement).checked)).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'chat_completions'
    ])
  })

	it('submits OpenAI quota auto-pause thresholds in extra', async () => {
	  const account = buildAccount()
	  account.extra = {
		auto_pause_5h_threshold: 0.9,
		auto_pause_7d_threshold: 0.8
	  }
	  updateAccountMock.mockReset()
	  checkMixedChannelRiskMock.mockReset()
	  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
	  updateAccountMock.mockResolvedValue(account)

	  const wrapper = mountModal(account)

	  await wrapper.get('[data-testid="auto-pause-5h-threshold"]').setValue('95')
	  await wrapper.get('[data-testid="auto-pause-7d-threshold"]').setValue('96')
	  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
	  await flushPromises()

	  expect(updateAccountMock).toHaveBeenCalledTimes(1)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_5h_threshold).toBe(0.95)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_7d_threshold).toBe(0.96)
	})

	it('submits OpenAI quota auto-pause disable flag in extra', async () => {
	  // Toggling the per-account disable flag must persist as auto_pause_5h_disabled
	  // so an admin can exempt one account from auto-pause even when a global default
	  // threshold is configured (otherwise leaving the threshold blank would silently
	  // fall back to the global default).
	  const account = buildAccount()
	  updateAccountMock.mockReset()
	  checkMixedChannelRiskMock.mockReset()
	  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
	  updateAccountMock.mockResolvedValue(account)

	  const wrapper = mountModal(account)

	  await wrapper.get('[data-testid="auto-pause-5h-disabled"]').trigger('click')
	  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
	  await flushPromises()

	  expect(updateAccountMock).toHaveBeenCalledTimes(1)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_5h_disabled).toBe(true)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_7d_disabled).toBeUndefined()
	})

  it('preserves Seedance when exactly two endpoint capabilities are selected', async () => {
    const account = buildAccount()
    account.credentials.openai_capabilities = ['chat_completions', 'seedance']
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)
    expect(wrapper.get<HTMLInputElement>('[data-testid="openai-endpoint-capability-seedance"]').element.checked).toBe(true)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual(['chat_completions', 'seedance'])
  })

  it('keeps at least one OpenAI APIKey endpoint capability selected', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const chatCheckbox = wrapper.get<HTMLInputElement>(
      '[data-testid="openai-endpoint-capability-chat_completions"]'
    )
    const embeddingsCheckbox = wrapper.get<HTMLInputElement>(
      '[data-testid="openai-endpoint-capability-embeddings"]'
    )

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(true)

    await embeddingsCheckbox.setValue(false)

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(false)

    await chatCheckbox.setValue(false)

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(false)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'chat_completions'
    ])
  })

  it('disables text generation protocol when only embeddings requests are accepted', async () => {
    const account = buildAccount()
    account.credentials.openai_capabilities = ['embeddings']
    account.extra = {
      openai_responses_mode: 'force_responses',
      openai_responses_supported: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const responsesModeSelect = wrapper.get<HTMLSelectElement>(
      '[data-testid="openai-responses-mode-select"]'
    )

    expect(responsesModeSelect.element.disabled).toBe(true)
    expect(wrapper.find('[data-testid="openai-responses-mode-not-applicable"]').exists()).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'embeddings'
    ])
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('openai_responses_mode')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(true)
  })

  it('submits Codex image tool force-inject mode as bridge override', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_bridge: false,
      codex_image_generation_bridge_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageTool')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolDesc')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolEnabledDesc')

    await wrapper.get('button[data-testid="codex-image-tool-enabled"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_bridge).toBe(true)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge_enabled')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
  })

  it('submits Codex image tool no-injection mode without strip policy', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('button[data-testid="codex-image-tool-disabled"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_bridge).toBe(false)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
  })

  it('submits Codex image tool block mode as strip policy and clears bridge override', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_bridge: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolBlock')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolBlockDesc')

    await wrapper.get('button[data-testid="codex-image-tool-block"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_explicit_tool_policy).toBe('strip')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge')
  })

  it('loads strip policy as block mode and clears both keys when reset to inherit', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_explicit_tool_policy: 'strip'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('button[data-testid="codex-image-tool-inherit"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge')
  })

  it('setup-token account can select and submit OAuth WS mode', async () => {
    const account = buildOpenAISetupTokenAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="edit-openai-ws-mode-select"]').setValue('http_bridge')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_oauth_responses_websockets_v2_mode).toBe('http_bridge')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_oauth_responses_websockets_v2_enabled).toBe(true)
  })

  it('allows saving apikey account when backend redacted api_key but credentials_status reports it exists', async () => {
    // 新前端 + 新后端：响应已脱敏，credentials 里没有 api_key，credentials_status.has_api_key=true
    const account = buildAccount()
    account.credentials = {
      base_url: 'https://api.openai.com',
      model_mapping: { 'gpt-5.2': 'gpt-5.2' }
    }
    account.credentials_status = { has_api_key: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    // 用户未输入新 key 时，payload 不应带 api_key，由后端合并保留旧值
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('api_key')
  })

  it('allows saving apikey account against legacy backend without credentials_status', async () => {
    // 新前端 + 旧后端：credentials_status 缺失，但 credentials.api_key 仍是明文，应允许保存
    const account = buildAccount()
    // 显式确保没有 credentials_status
    expect(account.credentials_status).toBeUndefined()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    // 旧后端响应未脱敏，原 api_key 会随 currentCredentials 一起传回去（旧行为，等价于无操作）
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.api_key).toBe('sk-test')
  })

  it('blocks apikey save when neither credentials_status nor legacy api_key indicates existence', async () => {
    const account = buildAccount()
    account.credentials = {
      base_url: 'https://api.openai.com'
    }
    // 既没有 credentials_status 也没有旧的 api_key
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).not.toHaveBeenCalled()
  })

  it('allows saving Vertex SA account when backend redacted service_account_json but credentials_status reports it exists', async () => {
    // 新前端 + 新后端：响应已脱敏，credentials 里没有 service_account_json，credentials_status.has_service_account_json=true
    const account = buildVertexAccount()
    account.credentials = {
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    }
    account.credentials_status = { has_service_account_json: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.project_id).toBe('demo-project')
  })

  it('allows saving Vertex SA account against legacy backend without credentials_status', async () => {
    // 新前端 + 旧后端：credentials_status 缺失，但 credentials.service_account_json 仍是明文，应允许保存
    const account = buildVertexAccount()
    expect(account.credentials_status).toBeUndefined()
    expect(account.credentials.service_account_json).toBeTruthy()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
  })

  it('shows quota limit card for Vertex SA account and keeps quota limits on save', async () => {
    const account = buildVertexAccount()
    account.extra = { quota_limit: 200, quota_daily_limit: 20, quota_used: 35.5 }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    expect(wrapper.findComponent({ name: 'QuotaLimitCard' }).exists()).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const extra = updateAccountMock.mock.calls[0]?.[1]?.extra
    expect(extra?.quota_limit).toBe(200)
    expect(extra?.quota_daily_limit).toBe(20)
    expect(extra?.quota_weekly_limit).toBeUndefined()
  })

  it('blocks Vertex SA save when neither credentials_status nor legacy json indicates existence', async () => {
    const account = buildVertexAccount()
    account.credentials = {
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    }
    // 既没有 credentials_status 也没有旧的 service_account_json
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).not.toHaveBeenCalled()
  })

  it('keeps the redacted Vertex API key when saving without entering a new one', async () => {
    const account = buildVertexAccount()
    account.credentials = {
      auth_mode: 'apikey',
      project_id: 'demo-project',
      location: 'us-central1',
      tier_id: 'vertex'
    }
    account.credentials_status = { has_api_key: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    expect(wrapper.find('[data-testid="edit-vertex-api-key-input"]').exists()).toBe(true)
    await wrapper.get('[data-testid="edit-vertex-api-key-project-id-input"]').setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toMatchObject({ auth_mode: 'apikey', location: 'us-central1', tier_id: 'vertex' })
    expect(credentials).not.toHaveProperty('api_key')
    expect(credentials).not.toHaveProperty('project_id')
  })

  it('submits a rotated Vertex API key', async () => {
    const account = buildVertexAccount()
    account.credentials = { auth_mode: 'apikey', location: 'global', tier_id: 'vertex' }
    account.credentials_status = { has_api_key: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="edit-vertex-api-key-input"]').setValue(' rotated-key ')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.api_key).toBe('rotated-key')
  })

  it('blocks Vertex API key save when no key exists and none is entered', async () => {
    const account = buildVertexAccount()
    account.credentials = { auth_mode: 'apikey', location: 'global', tier_id: 'vertex' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).not.toHaveBeenCalled()
  })

  it('loads and submits Antigravity configured project fallback', async () => {
    const account = buildAntigravityAccount('configured-project')
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('[data-testid="antigravity-project-id-input"]')
    expect(input.element.value).toBe('configured-project')

    await input.setValue('  updated-project  ')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.antigravity_project_id).toBe(
      'updated-project'
    )
  })

  it('clears Antigravity configured project fallback when input is empty', async () => {
    const account = buildAntigravityAccount('configured-project')
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('[data-testid="antigravity-project-id-input"]')

    await input.setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty(
      'antigravity_project_id'
    )
  })
})

describe('EditAccountModal OpenAI 自动使用重置卡', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('仅对 OpenAI OAuth 母账号显示，默认关闭且阈值为 100/100', () => {
    const parent = mountModal(buildOpenAIOAuthParentAccount())
    expect(parent.find('[data-testid="auto-reset-credit-settings"]').exists()).toBe(true)
    expect((parent.get('[data-testid="auto-reset-credit-5h-threshold"]').element as HTMLInputElement).value).toBe('100')
    expect((parent.get('[data-testid="auto-reset-credit-7d-threshold"]').element as HTMLInputElement).value).toBe('100')
    expect(parent.get('[data-testid="auto-reset-credit-5h-threshold"]').attributes('disabled')).toBeDefined()
    parent.unmount()

    for (const account of [buildAccount(), buildOpenAISetupTokenAccount(), buildOpenAISparkShadowAccount()]) {
      const wrapper = mountModal(account)
      expect(wrapper.find('[data-testid="auto-reset-credit-settings"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('独立保存两个阈值，并禁止把运行态回写到管理请求', async () => {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = {
      codex_auto_reset_credit_state: {
        status: 'success',
        trigger_window: '5h',
        available_count: 1
      }
    }
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="auto-reset-credit-enabled"]').trigger('click')
    await wrapper.get('[data-testid="auto-reset-credit-5h-threshold"]').setValue('75.5')
    await wrapper.get('[data-testid="auto-reset-credit-7d-threshold"]').setValue('92')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const extra = updateAccountMock.mock.calls[0]?.[1]?.extra
    expect(extra).toMatchObject({
      auto_reset_credit_enabled: true,
      auto_reset_credit_5h_threshold: 0.755,
      auto_reset_credit_7d_threshold: 0.92
    })
    expect(extra).not.toHaveProperty('codex_auto_reset_credit_state')
    wrapper.unmount()
  })

  it('开启后拒绝超出 0.1–100 范围的任一阈值', async () => {
    const wrapper = mountModal(buildOpenAIOAuthParentAccount())
    await wrapper.get('[data-testid="auto-reset-credit-enabled"]').trigger('click')
    await wrapper.get('[data-testid="auto-reset-credit-5h-threshold"]').setValue('0')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('delegated account editing', () => {
  it('updates basic fields without replacing projected credentials or extra', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await wrapper.setProps({ allowTransportEdit: false, allowAuthorize: false, allowRateEdit: false, showCost: false })
    await wrapper.get('form#edit-account-form input[type="text"]').setValue('operator name')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const payload = JSON.parse(JSON.stringify(updateAccountMock.mock.calls.at(-1)?.[1]))
    expect(payload.name).toBe('operator name')
    expect(payload).not.toHaveProperty('credentials')
    expect(payload).not.toHaveProperty('extra')
    expect(payload).not.toHaveProperty('proxy_id')
    expect(payload).not.toHaveProperty('rate_multiplier')
    wrapper.unmount()
  })
})

describe('EditAccountModal gemini explicit cache upstream switch', () => {
  function buildGeminiApiKeyAccount(credentials: Record<string, unknown>) {
    return {
      id: 21,
      name: 'Gemini upstream gateway',
      notes: '',
      platform: 'gemini',
      type: 'apikey',
      credentials: { api_key: 'sk-upstream', ...credentials },
      credentials_status: { has_api_key: true },
      extra: {},
      proxy_id: null,
      concurrency: 1,
      priority: 1,
      rate_multiplier: 1,
      status: 'active',
      group_ids: [],
      expires_at: null,
      auto_pause_on_expired: false
    } as any
  }

  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('hides the switch for the official AI Studio address', () => {
    const wrapper = mountModal(buildGeminiApiKeyAccount({ base_url: 'https://generativelanguage.googleapis.com' }))
    expect(wrapper.find('[data-testid="explicit-cache-upstream-section"]').exists()).toBe(false)
  })

  it('loads and saves the switch and ttl limit for a custom upstream', async () => {
    const account = buildGeminiApiKeyAccount({
      base_url: 'https://upstream.example.com',
      explicit_cache_upstream: true,
      explicit_cache_upstream_max_ttl_seconds: 1800
    })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="explicit-cache-upstream-section"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="explicit-cache-upstream-toggle"]').attributes('aria-checked')).toBe('true')
    const ttlInput = wrapper.get('[data-testid="explicit-cache-upstream-max-ttl"]')
    expect((ttlInput.element as HTMLInputElement).value).toBe('1800')

    await ttlInput.setValue('3600')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      explicit_cache_upstream: true,
      explicit_cache_upstream_max_ttl_seconds: 3600
    })
  })

  it('removes both fields when the switch is turned off', async () => {
    const account = buildGeminiApiKeyAccount({
      base_url: 'https://upstream.example.com',
      explicit_cache_upstream: true,
      explicit_cache_upstream_max_ttl_seconds: 1800
    })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="explicit-cache-upstream-toggle"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).not.toHaveProperty('explicit_cache_upstream')
    expect(credentials).not.toHaveProperty('explicit_cache_upstream_max_ttl_seconds')
  })
})

describe('EditAccountModal temp-unschedulable rule scope', () => {
  function buildAccountWithTempUnschedRules(rules: Array<Record<string, unknown>>) {
    const account = buildAccount()
    return {
      ...account,
      credentials: {
        ...account.credentials,
        temp_unschedulable_enabled: true,
        temp_unschedulable_rules: rules
      }
    } as any
  }

  const checkedScopes = (wrapper: ReturnType<typeof mountModal>) =>
    wrapper
      .findAll('[data-testid^="temp-unsched-scope-"][aria-checked="true"]')
      .map((radio) => radio.attributes('data-testid'))

  const savedRules = () =>
    updateAccountMock.mock.calls[0]?.[1]?.credentials?.temp_unschedulable_rules

  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('shows rules without scope the way the backend treats them and keeps that on save', async () => {
    const account = buildAccountWithTempUnschedRules([
      { error_code: 401, keywords: ['unauthorized'], duration_minutes: 10 },
      { error_code: 503, keywords: ['unavailable'], duration_minutes: 30 }
    ])
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    expect(checkedScopes(wrapper)).toEqual(['temp-unsched-scope-account', 'temp-unsched-scope-model'])

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(savedRules().map((rule: { scope: string }) => rule.scope)).toEqual(['account', 'model'])
    wrapper.unmount()
  })

  it('respects an explicit scope and saves the card the operator picks', async () => {
    const account = buildAccountWithTempUnschedRules([
      { error_code: 401, keywords: ['unauthorized'], duration_minutes: 10, scope: 'model' },
      { error_code: 503, keywords: ['unavailable'], duration_minutes: 30, scope: ' Account ' }
    ])
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    expect(checkedScopes(wrapper)).toEqual(['temp-unsched-scope-model', 'temp-unsched-scope-account'])

    await wrapper.findAll('[data-testid="temp-unsched-scope-model"]')[1].trigger('click')
    expect(checkedScopes(wrapper)).toEqual(['temp-unsched-scope-model', 'temp-unsched-scope-model'])

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(savedRules().map((rule: { scope: string }) => rule.scope)).toEqual(['model', 'model'])
    wrapper.unmount()
  })

  it('defaults a newly added rule to model scope', async () => {
    const wrapper = mountModal(buildAccountWithTempUnschedRules([]))

    const addRule = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.accounts.tempUnschedulable.addRule'))
    await addRule!.trigger('click')

    expect(checkedScopes(wrapper)).toEqual(['temp-unsched-scope-model'])
    wrapper.unmount()
  })
})

vi.mock('@/features/resources/accounts', () => ({ ...accountsApiFixture, default: accountsApiFixture, accountsAPI: accountsApiFixture }))

vi.mock('@/features/resources/services', async () => {
const actual = await vi.importActual<typeof import('@/features/resources/services')>('@/features/resources/services')
const { editorTestServices } = await import('../../__tests__/editorTestServices')
const { getPresetMappingsByPlatform } = await import('@/features/catalog')

const { useAppStore } = await import('@/app/model/app')
const { useAuthStore } = await import('@/features/identity')
return { ...actual, useResourceEditorServices: () => { const services = editorTestServices()
services.modelPresets = getPresetMappingsByPlatform
services.notices = { ...services.notices, ...useAppStore() }
const auth = useAuthStore()
services.access = { canEditAdminField: auth.canEditAdminField, get canManage() { return auth.can('accounts.manage') }, get isSuperAdmin() { return auth.isSuperAdmin }, get isSimpleMode() { return auth.isSimpleMode }, get canAuthorize() { return typeof auth.can === 'function' ? auth.can('accounts.authorize') : true } }




if (tlsFingerprintProfilesApiFixture?.list) services.tlsProfiles = tlsFingerprintProfilesApiFixture.list
if (settingsApiFixture?.getWebSearchEmulationConfig) services.webSearchConfig = settingsApiFixture.getWebSearchEmulationConfig
return services } }
})
