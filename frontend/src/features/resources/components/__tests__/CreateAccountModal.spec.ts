import { uiFeedbackKey } from '@/shared/ui/feedback'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  type PlatformCatalog,
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog,
} from '@/features/catalog'

const {
  createAccountMock,
  probeUpstreamBillingMock,
  syncUpstreamModelsMock,
  showWarningMock,
  importCodexSessionMock,
  createOpenAICodexPATMock,
  authIsSimpleMode,
  managedCatalogMock,
  showErrorMock,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
  syncUpstreamModelsMock: vi.fn(),
  showWarningMock: vi.fn(),
  importCodexSessionMock: vi.fn(),
  createOpenAICodexPATMock: vi.fn(),
  authIsSimpleMode: { value: true },
  managedCatalogMock: vi.fn().mockImplementation(async (platform: string) =>
    [...(platform === 'openai' ? ['gpt-reserve', 'gpt-6.1-sol'] : platform === 'anthropic' ? ['claude-haiku-5-5'] : platform === 'typesafe' ? ['jev-latest'] : []), 'disabled-model'].map(id => ({ id, enabled: id !== 'disabled-model', display_name: id }))),
  showErrorMock: vi.fn(),
}))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showWarning: showWarningMock,
  }),
}))

vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({
    isSuperAdmin: true,
    get isSimpleMode() {
      return authIsSimpleMode.value
    },
  }),
}))


const accountsApiFixture = vi.hoisted(() => ({
      creationDefaults: vi.fn().mockResolvedValue({ concurrency: 10, priority: 1 }),
      create: createAccountMock,
      probeUpstreamBilling: probeUpstreamBillingMock,
      syncUpstreamModels: syncUpstreamModelsMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
      importCodexSession: importCodexSessionMock,
      createOpenAICodexPAT: createOpenAICodexPATMock,
      getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({}),
    }))
const settingsApiFixture = vi.hoisted(() => ({
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({}),
    }))
const tlsFingerprintProfilesApiFixture = vi.hoisted(() => ({
      list: vi.fn().mockResolvedValue([]),
    }))




vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showEmailPasswordOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String,
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-codex-session', 'import-codex-pat'],
  template: `
    <div>
      <button data-testid="import-codex-session" @click="$emit('import-codex-session', 'session-json')">session</button>
      <button data-testid="import-codex-pat" @click="$emit('import-codex-pat', 'pat-token')">pat</button>
    </div>
  `,
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  template: `
    <button
      type="button"
      data-testid="select-pricing-groups"
      @click="$emit('update:modelValue', [1, 2])"
    >
      groups
    </button>
  `,
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
    platform: String,
  },
  emits: ['update:modelValue'],
  template: `<button
    type="button"
    data-testid="model-whitelist-selector"
    @click="$emit('update:modelValue', ['public-glm'])"
  >models</button>`,
})

function mountModal(groups: any[] = []) {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups },
    global: {
      provide: { [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } },
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
      },
    },
  })
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(
  platform: 'openai' | 'anthropic',
  enableLongContextBilling = false,
  disableUpstreamBillingProbe = false
) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, platform === 'openai' ? 'OpenAI' : 'admin.accounts.claudeConsole')
  if (platform === 'openai') {
    await selectButtonByText(wrapper, 'API Key')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} account`)
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  if (disableUpstreamBillingProbe) {
    await wrapper.get('[data-testid="upstream-billing-auto-probe"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

async function openCodexImportStep(toggleClicks = 0) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, 'OpenAI')
  for (let click = 0; click < toggleClicks; click += 1) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

describe('CreateAccountModal OpenAI long-context billing', () => {
  beforeEach(() => {
    accountsApiFixture.creationDefaults.mockReset().mockResolvedValue({ concurrency: 10, priority: 1 })
    managedCatalogMock.mockReset().mockImplementation(async (platform: string) =>
      [...(platform === 'openai' ? ['gpt-reserve', 'gpt-6.1-sol'] : platform === 'anthropic' ? ['claude-haiku-5-5'] : platform === 'typesafe' ? ['jev-latest'] : []), 'disabled-model'].map(id => ({ id, enabled: id !== 'disabled-model', display_name: id })))
    showErrorMock.mockReset()
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ models: [], metadata: {} })
    showWarningMock.mockReset()
    importCodexSessionMock.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      errors: [],
      warnings: [],
    })
    createOpenAICodexPATMock.mockReset().mockResolvedValue({})
  })

  afterEach(() => vi.useRealTimers())

  it('loads creation defaults when first mounted open by a lazy parent', async () => {
    accountsApiFixture.creationDefaults.mockResolvedValueOnce({ concurrency: 17, priority: 9 })
    const wrapper = mountModal()
    await flushPromises()
    expect(accountsApiFixture.creationDefaults).toHaveBeenCalledOnce()
    expect((wrapper.get('[data-tour="account-form-priority"]').element as HTMLInputElement).value).toBe('9')
    const concurrency = wrapper.findAll('input[type="number"]').find(input => input.element instanceof HTMLInputElement && input.element.value === '17')
    expect(concurrency).toBeDefined()
    wrapper.unmount()
  })

  it('does not apply defaults after a lazily mounted editor closes', async () => {
    let finish!: (value: { concurrency: number; priority: number }) => void
    accountsApiFixture.creationDefaults.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mountModal()
    await wrapper.setProps({ show: false })
    finish({ concurrency: 17, priority: 9 })
    await flushPromises()
    accountsApiFixture.creationDefaults.mockResolvedValueOnce({ concurrency: 10, priority: 3 })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect((wrapper.get('[data-tour="account-form-priority"]').element as HTMLInputElement).value).toBe('3')
    wrapper.unmount()
  })

  it('does not fetch Antigravity defaults while closing an unrelated editor', async () => {
    const { getAntigravityDefaultModelMapping } = await import('@/features/resources/accounts')
    vi.mocked(getAntigravityDefaultModelMapping).mockClear()
    const wrapper = mountModal()
    await wrapper.setProps({ show: false })
    await flushPromises()
    expect(getAntigravityDefaultModelMapping).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reports an Antigravity defaults failure without publishing a fallback mapping', async () => {
    const { getAntigravityDefaultModelMapping } = await import('@/features/resources/accounts')
    vi.mocked(getAntigravityDefaultModelMapping).mockRejectedValueOnce(new Error('Unavailable'))
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Antigravity')
    await flushPromises()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.failedToLoad')
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })


  it('creates a Grok API-key resource with the official endpoint through the real form', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Grok')
    await wrapper.get('[data-testid="grok-account-type-api-key"]').trigger('click')
    await flushPromises()
    const keyInput = wrapper.get('form input[type="password"]')
    expect(keyInput.attributes('placeholder')).toBe('xai-...')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok resource')
    await keyInput.setValue('test-xai-key')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock.mock.calls[0][0]).toMatchObject({ platform: 'grok', type: 'apikey', credentials: { api_key: 'test-xai-key', base_url: 'https://api.x.ai/v1' } })
    wrapper.unmount()
  })

  it('keeps Grok upstream configuration distinct from resource OAuth and hides password authorization', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Grok')
    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="grok-custom-base-url-input"]').exists()).toBe(true)
    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok OAuth')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.findComponent(OAuthAuthorizationFlowStub).props('showEmailPasswordOption')).toBe(false)
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })



  it('loads the shared built-in defaults on opening and switching platforms', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await selectButtonByText(wrapper, 'admin.accounts.claudeConsole')
    await flushPromises()
    expect(wrapper.findComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['claude-haiku-5-5'])
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await flushPromises()
    expect(managedCatalogMock).toHaveBeenLastCalledWith('openai', { signal: expect.any(AbortSignal) })
    expect(wrapper.findComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['gpt-reserve', 'gpt-6.1-sol'])
  })

  it('submits the shared OpenAI defaults without requiring a sync-button click', async () => {
    await submitApiKeyAccount('openai')
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials?.model_allowlist).toEqual(['gpt-reserve', 'gpt-6.1-sol'])
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).not.toHaveProperty('model_mapping')
  })

  it('does not reseed cancelled models when switching between whitelist and mapping tabs', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI'); await selectButtonByText(wrapper, 'API Key'); await flushPromises()
    wrapper.getComponent(ModelWhitelistSelectorStub).vm.$emit('update:modelValue', ['gpt-reserve'])
    await selectButtonByText(wrapper, 'admin.accounts.modelMapping')
    await selectButtonByText(wrapper, 'admin.accounts.modelWhitelist'); await flushPromises()
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['gpt-reserve'])
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('modelValue')).not.toContain('disabled-model')
    wrapper.unmount()
  })

  it('does not submit while the default model catalog is loading', async () => {
    let complete!: (models: { id: string; enabled: boolean; display_name: string }[]) => void
    managedCatalogMock.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = mountModal()
    await wrapper.get('form#create-account-form input[type="text"]').setValue('pending')
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('[data-testid="import-codex-session"]').exists()).toBe(false)
    complete([{ id: 'claude-haiku-5-5', enabled: true, display_name: 'Haiku' }])
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeUndefined()
  })

  it.each(['switch', 'close', 'edit'])('discards pending default models after %s', async (action) => {
    let complete!: (models: { id: string; enabled: boolean; display_name: string }[]) => void
    managedCatalogMock.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = mountModal()
    if (action === 'switch') await selectButtonByText(wrapper, 'OpenAI')
    else if (action === 'close') await wrapper.setProps({ show: false })
    else {
      await selectButtonByText(wrapper, 'admin.accounts.claudeConsole')
      await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    }
    complete([{ id: 'stale-model', enabled: true, display_name: 'Stale' }])
    await flushPromises()
    if (action === 'close') {
      await wrapper.setProps({ show: true })
      await flushPromises()
      await selectButtonByText(wrapper, 'admin.accounts.claudeConsole')
    } else if (action === 'switch') await selectButtonByText(wrapper, 'API Key')
    const models = wrapper.findComponent(ModelWhitelistSelectorStub).props('modelValue')
    expect(models).not.toContain('stale-model')
    if (action === 'edit') expect(models).toEqual(['public-glm'])
  })

  it('does not fall back to a static list when the default catalog fails', async () => {
    managedCatalogMock.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'admin.accounts.claudeConsole')
    await flushPromises()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.modelReference.loadFailed')
    expect(wrapper.findComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual([])
  })

  it('sets month and year expiry presets without submitting the account form', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-01-31T12:34:00'))
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('expiry account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')

    for (const [label, expected] of [
      ['payment.oneMonth', '2026-02-28T12:34'],
      ['payment.oneYear', '2027-01-31T12:34'],
    ]) {
      const button = wrapper.findAll('button').find((candidate) => candidate.text() === label)!
      expect(button.attributes('type')).toBe('button')
      await button.trigger('click')
      expect(input.element.value).toBe(expected)
      expect(createAccountMock).not.toHaveBeenCalled()
    }

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]?.expires_at).toBe(new Date('2027-01-31T12:34:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('allows a manually entered expiry to override a preset before account creation', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('custom expiry account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await selectButtonByText(wrapper, 'payment.oneMonth')
    await wrapper.get('input[type="datetime-local"]').setValue('2030-04-15T09:20')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]?.expires_at).toBe(new Date('2030-04-15T09:20:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('hides only the redundant account toggle when every selected group enables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: true },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('keeps the account toggle when any selected group disables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: false },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('sends false explicitly for normal OpenAI account creation by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('omits the upstream request id header from extra when left empty', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra).not.toHaveProperty('upstream_request_id_header')
  })

  it('sends the trimmed upstream request id header in extra when filled', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('openai account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue('  X-Oneapi-Request-Id  ')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.upstream_request_id_header).toBe('X-Oneapi-Request-Id')
  })

  it('omits images_url_to_b64_json from extra by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra).not.toHaveProperty('images_url_to_b64_json')
  })

  it('sends images_url_to_b64_json in extra when the toggle is enabled', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('openai account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.images_url_to_b64_json).toBe(true)
  })

  it('saves managed references without invoking the retired per-account sync', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenCode account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledOnce()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
  })

  it('keeps the directory selector free of credential preview payloads', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await flushPromises()

    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toBeUndefined()
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('modelValue')).toEqual(['public-glm'])
  })

  it('preserves a real rewrite independently of managed references', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Mapped account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await selectButtonByText(wrapper, 'admin.accounts.modelMapping')
    await selectButtonByText(wrapper, 'admin.accounts.addMapping')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('public-glm')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('glm-5.3')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials?.model_mapping).toEqual({
      'public-glm': 'glm-5.3'
    })
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
  })

  it('does not invoke the retired sync warning path for managed platforms', async () => {
    syncUpstreamModelsMock.mockResolvedValue({
      models: ['x-preview-f-free'],
      warnings: [{ code: 'upstream_model_metadata_incomplete', message: 'metadata incomplete' }],
    })
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenCode account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(showWarningMock).not.toHaveBeenCalled()
  })

  // namespace 摊平是仅 OAuth 的兼容开关：API Key 走 chat completions 回退桥时由桥自行摊平
  it('shows the Codex namespace flatten toggle only for OpenAI OAuth accounts', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')

    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      true
    )

    await selectButtonByText(wrapper, 'API Key')
    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      false
    )
  })

  it('enables upstream billing probes by default for new OpenAI API key accounts', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('waits for the initial upstream billing probe before refreshing the account list', async () => {
    let resolveProbe: (() => void) | undefined
    probeUpstreamBillingMock.mockImplementationOnce(
      () => new Promise<void>((resolve) => {
        resolveProbe = resolve
      })
    )

    const wrapper = await submitApiKeyAccount('openai')

    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('created')).toBeUndefined()

    resolveProbe?.()
    await flushPromises()

    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it('sends an explicit disabled state when the create toggle is turned off', async () => {
    await submitApiKeyAccount('openai', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
    expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
  })

  it('submits OpenCode Zen default protocol rules with adaptive endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenCode')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('oc')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-opencode-zen')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
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
        { pattern: 'qwen3.8-max', protocol: 'chat_completions' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    })
  })

  it('submits OpenCode GO endpoints after switching account type', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenCode')
    await selectButtonByText(wrapper, 'admin.accounts.opencodeGo.accountMode.go')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('oc-go')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-opencode-go')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'go',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/go/v1',
        anthropic: 'https://opencode.ai/zen/go',
        responses: 'https://opencode.ai/zen/go/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'minimax-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    })
  })

  describe('providers using the generic form', () => {
    const serverOnlyProviders: PlatformCatalog = {
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
                  anthropic: 'https://api.acme-router.example/provider',
                },
                protocol_rules: [{ pattern: 'claude-*', protocol: 'anthropic' }],
              },
              {
                mode: 'team',
                base_urls: {
                  chat_completions: 'https://team.acme-router.example/provider/v1',
                  anthropic: 'https://team.acme-router.example/provider',
                },
                protocol_rules: [{ pattern: 'sonnet-*', protocol: 'anthropic' }],
              },
            ],
          },
        },
        {
          id: 'acme_chat',
          display_name: 'Acme Chat',
          gateway: 'openai',
          cn_provider: false,
          multi_protocol: {
            default_mode: 'pass',
            routing: 'by_inbound',
            modes: [{ mode: 'pass', base_urls: { chat_completions: 'https://api.acme-chat.example/v1' } }],
          },
        },
      ],
      composite_precedence: [...BUILTIN_PLATFORM_CATALOG.composite_precedence, 'acme_router', 'acme_chat'],
    }

    beforeEach(() => {
      setPlatformCatalog(serverOnlyProviders)
    })

    afterEach(() => {
      resetPlatformCatalog()
    })

    it('creates a by-model provider account from its profile defaults', async () => {
      const wrapper = mountModal()
      await wrapper.get('[data-testid="platform-button-acme_router"]').trigger('click')
      await wrapper.get('form#create-account-form input[type="text"]').setValue('cc')
      await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-cc')

      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      const payload = createAccountMock.mock.calls[0]?.[0]
      expect(payload?.platform).toBe('acme_router')
      expect(payload?.type).toBe('apikey')
      expect(payload?.credentials).toMatchObject({
        api_key: 'sk-cc',
        account_mode: 'standard',
        api_protocol: 'adaptive',
        base_url: 'https://api.acme-router.example/provider/v1',
        api_base_urls: {
          chat_completions: 'https://api.acme-router.example/provider/v1',
          anthropic: 'https://api.acme-router.example/provider',
        },
        protocol_rules: [{ pattern: 'claude-*', protocol: 'anthropic' }],
      })
      // 该供应商没有原生 Responses 端点，不下发 responses 基址。
      expect(payload?.credentials?.api_base_urls).not.toHaveProperty('responses')
      // 没有内置模型列表时不预填白名单，新账号不限制模型。
      expect(payload?.credentials).not.toHaveProperty('model_mapping')
    })

    it('switches endpoints and default rules with the provider mode', async () => {
      const wrapper = mountModal()
      await wrapper.get('[data-testid="platform-button-acme_router"]').trigger('click')
      await wrapper.get('[data-testid="generic-account-mode"]').findAll('button')[1].trigger('click')
      await wrapper.get('form#create-account-form input[type="text"]').setValue('cc-team')
      await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-cc')

      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
        account_mode: 'team',
        base_url: 'https://team.acme-router.example/provider/v1',
        api_base_urls: {
          chat_completions: 'https://team.acme-router.example/provider/v1',
          anthropic: 'https://team.acme-router.example/provider',
        },
        protocol_rules: [{ pattern: 'sonnet-*', protocol: 'anthropic' }],
      })
    })

    it('creates a by-inbound provider account without protocol rules', async () => {
      const wrapper = mountModal()
      await wrapper.get('[data-testid="platform-button-acme_chat"]').trigger('click')
      // 单一接入模式时不显示模式选择。
      expect(wrapper.find('[data-testid="generic-account-mode"]').exists()).toBe(false)
      await wrapper.get('form#create-account-form input[type="text"]').setValue('acme-chat')
      await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-acme-chat')

      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
      expect(credentials).toMatchObject({
        account_mode: 'pass',
        api_protocol: 'adaptive',
        base_url: 'https://api.acme-chat.example/v1',
        api_base_urls: { chat_completions: 'https://api.acme-chat.example/v1' },
      })
      expect(credentials).not.toHaveProperty('protocol_rules')
    })

    it('falls back to the Kimi default mode after a server-only provider', async () => {
      const wrapper = mountModal()
      await wrapper.get('[data-testid="platform-button-acme_router"]').trigger('click')
      await selectButtonByText(wrapper, 'Kimi')
      await wrapper.get('form#create-account-form input[type="text"]').setValue('kimi')
      await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-kimi')

      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
        account_mode: 'payg',
        base_url: 'https://api.moonshot.cn/v1',
      })
      expect(createAccountMock.mock.calls[0]?.[0]?.credentials).not.toHaveProperty('protocol_rules')
    })
  })

  it('groups the aggregators on their own row below the CN providers', () => {
    const wrapper = mountModal()
    const labels = (testid: string) =>
      wrapper.get(`[data-testid="${testid}"]`).findAll('button').map(button => button.text().trim())
    expect(labels('platform-row-cn')).toEqual(['Kimi', 'Zhipu GLM', 'DeepSeek', 'MiniMax'])
    expect(labels('platform-row-aggregators')).toEqual(['OpenCode', 'Command Code', 'Cline'])
  })

  it('creates a Cline account without an account type and with only the Chat Completions endpoint', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="platform-button-cline"]').trigger('click')
    // 积分与 ClinePass 共用同一个 Key，按模型计费，不需要选择账号类型。
    expect(wrapper.find('[data-testid="generic-account-mode"]').exists()).toBe(false)
    await wrapper.get('form#create-account-form input[type="text"]').setValue('cline')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-cline')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload?.platform).toBe('cline')
    expect(payload?.credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.cline.bot/api/v1',
      api_base_urls: { chat_completions: 'https://api.cline.bot/api/v1' },
    })
    expect(payload?.credentials).not.toHaveProperty('protocol_rules')
    expect(payload?.credentials?.api_base_urls).not.toHaveProperty('responses')
    expect(payload?.credentials?.api_base_urls).not.toHaveProperty('anthropic')
  })

  it('submits adaptive Kimi protocol endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Kimi adaptive')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-kimi')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
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

  it('submits adaptive Kimi Coding Plan Responses endpoint', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.coding')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Kimi coding')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-kimi-coding')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://api.kimi.com/coding/v1',
      api_base_urls: {
        chat_completions: 'https://api.kimi.com/coding/v1',
        anthropic: 'https://api.kimi.com/coding',
        responses: 'https://api.kimi.com/coding/v1'
      }
    })
  })

  it('submits adaptive MiniMax protocol endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'MiniMax')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('MiniMax adaptive')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-minimax')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.minimaxi.com/v1',
      api_base_urls: {
        chat_completions: 'https://api.minimaxi.com/v1',
        anthropic: 'https://api.minimaxi.com/anthropic',
        responses: 'https://api.minimaxi.com/v1'
      }
    })
  })

  it('saves the edited adaptive Chat endpoint without a directory credential preview', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await wrapper
      .get('[data-testid="cn-adaptive-base-url-chat_completions"]')
      .setValue('https://relay.example.com/v1')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-relay')

    await wrapper.get('form#create-account-form input[type="text"]').setValue('Adaptive account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent'); await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({ base_url: 'https://relay.example.com/v1', api_key: 'sk-relay' })
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toBeUndefined()
  })

  it('exposes Agent Identity in the OpenAI authorization methods', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    expect(flow.props('showManualOption')).toBe(true)
    expect(flow.props('showCodexSessionImportOption')).toBe(true)
    expect(flow.props('showAgentIdentityOption')).toBe(true)
    expect(flow.props('showCodexPatOption')).toBe(true)
    expect(flow.props('initialInputMethod')).toBe('manual')
  })

  it.each([
    ['camelCase', { authMode: 'agentIdentity', agentIdentity: { agentRuntimeId: 'runtime' } }],
    ['nested identity without auth_mode', { agent_identity: { agent_runtime_id: 'runtime' } }],
  ])('accepts backend-compatible %s Agent Identity imports', async (_name, content) => {
    const wrapper = await openCodexImportStep()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.inputMethod = 'agent_identity'

    flow.vm.$emit('import-codex-session', JSON.stringify(content))
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
  })

  it.each(['session', 'pat'])('sends independent model references through the OpenAI %s OAuth import payload', async kind => {
    const wrapper = await openCodexImportStep()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.$emit(kind === 'session' ? 'import-codex-session' : 'import-codex-pat', kind === 'session' ? 'session-json' : 'pat-token')
    await flushPromises()
    const request = kind === 'session' ? importCodexSessionMock.mock.calls[0]?.[0] : createOpenAICodexPATMock.mock.calls[0]?.[0]
    expect(request?.credential_extras?.model_allowlist).toEqual(['gpt-reserve', 'gpt-6.1-sol'])
    expect(request?.credential_extras).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })

  it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
    await submitApiKeyAccount('openai', true)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('omits the OpenAI setting for non-OpenAI account creation', async () => {
    await submitApiKeyAccount('anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
    // 上游倍率探测已放宽到全部 API-key 平台：非 OpenAI 平台与 OpenAI 一致，默认开启。
    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('sends an explicit disabled state when the non-OpenAI create toggle is turned off', async () => {
    await submitApiKeyAccount('anthropic', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
  })

  it('antigravity upstream 创建默认携带上游倍率探测开关', async () => {
    // antigravity upstream 走独立创建 helper，
    // 也必须与其余 API-key 平台一样默认开启探测并传递开关。
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Antigravity')
    await selectButtonByText(wrapper, 'admin.accounts.types.antigravityApikey')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('antigravity relay')
    const baseInput = wrapper
      .findAll('input')
      .find((candidate) => candidate.attributes('placeholder') === 'https://cloudcode-pa.googleapis.com')
    expect(baseInput).toBeDefined()
    await baseInput?.setValue('https://relay.example')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-upstream')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload?.platform).toBe('antigravity')
    expect(payload?.type).toBe('apikey')
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    // 创建成功后前端立即发起一次首探（与其他 apikey 平台一致）。
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
  })

  it('does not preselect BPS models during native OAuth account creation', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()
    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    const extra = importCodexSessionMock.mock.calls[0]?.[0]?.extra ?? {}
    expect(extra).not.toHaveProperty('openai_excel_bps_models')
    expect(extra).not.toHaveProperty('all_models')
    expect(extra).not.toHaveProperty('bps_all_models')
    expect(extra).not.toHaveProperty('openai_excel_bps', true)
    wrapper.unmount()
  })

  it('leaves Codex session import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('leaves Codex PAT import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('sends explicit true for Codex session import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex session import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('sends explicit true for Codex PAT import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex PAT import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })
})

describe('delegated account creation', () => {
  it('creates an API-key account without sending defaults for ungranted financial fields', async () => {
    createAccountMock.mockResolvedValue({ id: 42 })
    const wrapper = mountModal()
    await wrapper.setProps({ allowRateEdit: false, allowSystemSettings: false })
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('delegated account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('new-key')
    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="upstream-billing-auto-probe"]').exists()).toBe(false)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    const payload = JSON.parse(JSON.stringify(createAccountMock.mock.calls.at(-1)?.[0]))
    expect(payload.name).toBe('delegated account')
    expect(payload.rate_multiplier).toBeUndefined()
    expect(payload.upstream_billing_probe_enabled).toBeUndefined()
    expect(payload.extra?.openai_long_context_billing_enabled).toBeUndefined()
    expect(payload.credentials.api_key).toBe('new-key')
    wrapper.unmount()
  })
})

describe('CreateAccountModal gemini explicit cache upstream switch', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 43, platform: 'gemini', type: 'apikey' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ models: [], metadata: {} })
  })

  it('only offers the switch for a custom base url and sends it on create', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.gemini.accountType.apiKeyTitle')
    expect(wrapper.find('[data-testid="explicit-cache-upstream-section"]').exists()).toBe(false)

    const baseUrlInput = wrapper
      .findAll('form#create-account-form input[type="text"]')
      .find((input) => (input.element as HTMLInputElement).value === 'https://generativelanguage.googleapis.com')
    expect(baseUrlInput).toBeDefined()
    await baseUrlInput!.setValue('https://upstream.example.com')
    expect(wrapper.find('[data-testid="explicit-cache-upstream-section"]').exists()).toBe(true)

    await wrapper.get('[data-testid="explicit-cache-upstream-toggle"]').trigger('click')
    await wrapper.get('[data-testid="explicit-cache-upstream-max-ttl"]').setValue('1800')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('gemini upstream gateway')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-upstream')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      base_url: 'https://upstream.example.com',
      explicit_cache_upstream: true,
      explicit_cache_upstream_max_ttl_seconds: 1800
    })
  })

  it('only offers the AI Studio tier for the official base url', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.gemini.accountType.apiKeyTitle')
    expect(wrapper.find('[data-testid="gemini-oauth-tier"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gemini-apikey-note"]').exists()).toBe(true)
    await wrapper.get('[data-testid="gemini-apikey-tier"]').setValue('aistudio_paid')

    const baseUrlInput = wrapper
      .findAll('form#create-account-form input[type="text"]')
      .find((input) => (input.element as HTMLInputElement).value === 'https://generativelanguage.googleapis.com')
    expect(baseUrlInput).toBeDefined()
    await baseUrlInput!.setValue('https://upstream.example.com')
    expect(wrapper.find('[data-testid="gemini-apikey-tier"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gemini-apikey-note"]').exists()).toBe(false)

    await wrapper.get('form#create-account-form input[type="text"]').setValue('gemini relay')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-upstream')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
    expect(credentials).toMatchObject({ base_url: 'https://upstream.example.com' })
    expect(credentials).not.toHaveProperty('tier_id')
  })

  it('shows the OAuth tier fallback only on the OAuth form', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.gemini.accountType.oauthTitle')
    expect(wrapper.find('[data-testid="gemini-oauth-tier"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gemini-apikey-note"]').exists()).toBe(false)
  })

  it('keeps the selected AI Studio tier for the official base url', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.gemini.accountType.apiKeyTitle')
    await wrapper.get('[data-testid="gemini-apikey-tier"]').setValue('aistudio_paid')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('gemini official')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('AIza-official')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      base_url: 'https://generativelanguage.googleapis.com',
      tier_id: 'aistudio_paid'
    })
  })
})

describe('CreateAccountModal Vertex API key auth', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 44, platform: 'gemini', type: 'service_account' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ models: [], metadata: {} })
  })

  async function openGeminiVertexApiKeyForm() {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.vertexLabel')
    await wrapper.get('[data-testid="vertex-auth-mode-apikey"]').setValue(true)
    await wrapper.get('form#create-account-form input[type="text"]').setValue('vertex api key account')
    await wrapper.get('[data-testid="vertex-api-key-input"]').setValue(' vertex-key ')
    return wrapper
  }

  it('creates a Gemini Vertex account with API key auth and no project for the express endpoint', async () => {
    const wrapper = await openGeminiVertexApiKeyForm()
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload?.platform).toBe('gemini')
    expect(payload?.type).toBe('service_account')
    expect(payload?.credentials).toEqual({
      auth_mode: 'apikey',
      api_key: 'vertex-key',
      location: 'global',
      tier_id: 'vertex'
    })
  })

  it('sends the project id when an API key account targets a project endpoint', async () => {
    const wrapper = await openGeminiVertexApiKeyForm()
    await wrapper.get('[data-testid="vertex-api-key-project-id-input"]').setValue(' my-project ')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      auth_mode: 'apikey',
      api_key: 'vertex-key',
      project_id: 'my-project',
      location: 'global'
    })
  })

  it('keeps Anthropic Vertex accounts on Service Account JSON only', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'admin.accounts.vertexLabel')
    expect(wrapper.find('[data-testid="vertex-auth-mode-apikey"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="vertex-api-key-input"]').exists()).toBe(false)
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
services.access = { canManage: true, get isSuperAdmin() { return auth.isSuperAdmin }, get isSimpleMode() { return auth.isSimpleMode }, get canAuthorize() { return typeof auth.can === 'function' ? auth.can('accounts.authorize') : true } }
services.managedModelCatalog = managedCatalogMock




if (tlsFingerprintProfilesApiFixture?.list) services.tlsProfiles = tlsFingerprintProfilesApiFixture.list
if (settingsApiFixture?.getWebSearchEmulationConfig) services.webSearchConfig = settingsApiFixture.getWebSearchEmulationConfig
return services } }
})
