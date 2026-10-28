import { commandFixture, translate } from './boundaryFixtures'
import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent, h, reactive, ref, computed } from 'vue'
import { mount } from '@vue/test-utils'
import { createResourceTaskScope } from '../../queries/resourceTaskScope'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { useCreateAuthorizationDraft } from '../createAuthorization'
import { useCreateOpenAIAuthorizationDraft } from '../createOpenAIAuthorization'

const api = vi.hoisted(() => ({ generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), create: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
beforeEach(() => { vi.resetAllMocks() })

it.each(['proxy', 'category', 'platform', 'unchanged'] as const)('binds authorization to the original %s context before the second write', async transition => {
  let resolveToken!: (value: object) => void
  api.exchangeCode.mockReturnValue(new Promise(resolve => { resolveToken = resolve }))
  api.generateAuthUrl.mockResolvedValue({ auth_url: 'https://auth.example.invalid/?state=fixture', session_id: 'fixture-session' })
  api.create.mockResolvedValue({ id: 7 })
  const form = reactive({ platform: 'openai' as AccountPlatform, proxy_id: 11, name: 'fixture', notes: '', concurrency: 1, load_factor: null, priority: 50, rate_multiplier: 1, group_ids: [3], expires_at: null })
  const category = ref<'apikey' | 'bedrock' | 'service_account' | 'oauth-based'>('oauth-based')
  const resourceTasks = createResourceTaskScope(() => 'admin-session')
  const services = editorTestServices()
  const emit = vi.fn()
  const close = vi.fn()
  let authorization!: ReturnType<typeof useCreateAuthorizationDraft>
  let exchange!: ReturnType<typeof useCreateOpenAIAuthorizationDraft>['handleOpenAIExchange']
  const wrapper = mount(defineComponent({ setup() {
    // Only live authorization context and exchange/create paths are exercised.
    const input = { resourceTasks, form, t: translate, antigravityAccountType: ref<'oauth' | 'upstream'>('oauth'), accountCategory: category, addMethod: ref<'oauth' | 'setup-token' | 'setup-token-manual'>('oauth'), geminiAIStudioOAuthEnabled: ref(false), appStore: services.notices, step: ref(2), geminiSelectedTier: computed(() => '' as const) }
    authorization = useCreateAuthorizationDraft(commandFixture<Parameters<typeof useCreateAuthorizationDraft>[0]>(input))
    const flow = { resourceTasks, form, openaiOAuth: authorization.openaiOAuth, oauthFlowRef: authorization.oauthFlowRef,
      buildOpenAIExtra: () => undefined, openAILongContextBillingTouched: ref(false), isOpenAIModelRestrictionDisabled: computed(() => true), modelRestrictionMode: ref<"whitelist" | "mapping">("whitelist"), allowedModels: ref<string[]>([]), modelMappings: ref<{ from: string; to: string }[]>([]),
      buildOpenAICompactModelMapping: () => null, applyTempUnschedConfig: () => true, withAccountExtra: (value: Record<string, unknown> | undefined) => value,
      props: { allowRateEdit: true }, autoPauseOnExpired: ref(false), appStore: services.notices, t: translate, emit, handleClose: close,
    }
    exchange = useCreateOpenAIAuthorizationDraft(flow as Parameters<typeof useCreateOpenAIAuthorizationDraft>[0]).handleOpenAIExchange
    return () => h('div')
  } }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
  try {
    await authorization.handleGenerateUrl()
    const waiting = exchange('fixture-code')
    expect(api.exchangeCode).toHaveBeenCalledWith('/admin/openai/exchange-code', { code: 'fixture-code', session_id: 'fixture-session', state: 'fixture', proxy_id: 11 })
    if (transition === 'proxy') form.proxy_id = 22
    if (transition === 'category') category.value = 'apikey'
    if (transition === 'platform') form.platform = 'anthropic'
    resolveToken({ access_token: 'fixture-access', refresh_token: 'fixture-refresh', expires_at: 123 })
    await waiting
    if (transition === 'unchanged') {
      expect(api.create).toHaveBeenCalledTimes(1)
      expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ platform: 'openai', proxy_id: 11, group_ids: [3], credentials: expect.objectContaining({ refresh_token: 'fixture-refresh' }) }))
    } else {
      expect(api.create).not.toHaveBeenCalled()
      expect(emit).not.toHaveBeenCalled()
      expect(close).not.toHaveBeenCalled()
      expect(authorization.currentAuthUrl.value).toBe('')
    }
  } finally { wrapper.unmount(); resourceTasks.dispose() }
})
