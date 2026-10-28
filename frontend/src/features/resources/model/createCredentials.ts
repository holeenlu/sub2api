import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type AddMethod } from '@/features/resources/queries/useAccountOAuth'
import { type CnApiProtocol, type OpenCodeAccountMode } from "@/features/resources/model/credentialsBuilder"
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'




interface CreateCredentialsInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  form: Pick<CreateResourceForm, "platform">
  t: ReturnType<typeof useI18n>["t"]
  isMultiProtocolPlatform: ComputedRef<boolean>
  openCodeAccountMode: Ref<OpenCodeAccountMode, OpenCodeAccountMode>
  accountMode: Ref<string, string>
  defaultCNBaseUrl: ReturnType<typeof createProviderCredentialOptions>["defaultCNBaseUrl"]
  apiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  geminiOAuthType: Ref<"code_assist" | "google_one" | "ai_studio", "code_assist" | "google_one" | "ai_studio">
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
}




export function useCreateCredentialsDraft(input: CreateCredentialsInput): {
  baseUrlHint: ComputedRef<string>
  apiKeyHint: ComputedRef<string>
  apiKeyBaseUrlPlaceholder: ComputedRef<string>
  apiKeyValuePlaceholder: ComputedRef<"sk-proj-..." | "AQ.Ab..." | "xai-..." | "sk-..." | "<api-key>.<secret>" | "ts-..." | "sk-ant-...">
  step: Ref<number, number>
  accountCategory: Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">
  addMethod: Ref<AddMethod, AddMethod>
  apiKeyBaseUrl: Ref<string, string>
  apiKeyValue: Ref<string, string>
  antigravityAccountType: Ref<"oauth" | "upstream", "oauth" | "upstream">
  antigravityProjectId: Ref<string, string>
  upstreamBaseUrl: Ref<string, string>
  upstreamApiKey: Ref<string, string>
  bedrockAuthMode: Ref<"apikey" | "sigv4", "apikey" | "sigv4">
  bedrockAccessKeyId: Ref<string, string>
  bedrockSecretAccessKey: Ref<string, string>
  bedrockSessionToken: Ref<string, string>
  bedrockRegion: Ref<string, string>
  bedrockForceGlobal: Ref<boolean, boolean>
  bedrockApiKeyValue: Ref<string, string>
  vertexServiceAccountFileInput: Ref<HTMLInputElement | null, HTMLInputElement | null>
  vertexServiceAccountJson: Ref<string, string>
  vertexProjectId: Ref<string, string>
  vertexClientEmail: Ref<string, string>
  vertexLocation: Ref<string, string>
  vertexServiceAccountDragActive: Ref<boolean, boolean>
  vertexAuthMode: Ref<"apikey" | "service_account", "apikey" | "service_account">
  vertexApiKey: Ref<string, string>
  vertexUsesApiKey: ComputedRef<boolean>
  geminiAIStudioOAuthEnabled: Ref<boolean, boolean>
  showAdvancedOAuth: Ref<boolean, boolean>
  showGeminiHelpDialog: Ref<boolean, boolean>
  geminiTierGoogleOne: Ref<"google_one_free" | "google_ai_pro" | "google_ai_ultra", "google_one_free" | "google_ai_pro" | "google_ai_ultra">
  geminiTierGcp: Ref<"gcp_standard" | "gcp_enterprise", "gcp_standard" | "gcp_enterprise">
  geminiTierAIStudio: Ref<"aistudio_free" | "aistudio_paid", "aistudio_free" | "aistudio_paid">
  geminiSelectedTier: ComputedRef<"" | "aistudio_free" | "aistudio_paid" | "google_one_free" | "google_ai_pro" | "google_ai_ultra" | "gcp_standard" | "gcp_enterprise">
  applyVertexServiceAccountJson: (value: string) => boolean
  parseVertexServiceAccountJson: () => boolean
  handleVertexServiceAccountFile: (event: Event) => Promise<void>
  handleVertexServiceAccountDrop: (event: DragEvent) => Promise<void>
} {


  const baseUrlHint = computed(() => {
    if (input.form.platform === 'openai') return input.t('admin.accounts.openai.baseUrlHint')
    if (input.form.platform === 'gemini') return input.t('admin.accounts.gemini.baseUrlHint')
    if (input.form.platform === 'grok') return ''
    return input.t('admin.accounts.baseUrlHint')
  })


  const apiKeyHint = computed(() => {
    if (input.form.platform === 'openai') return input.t('admin.accounts.openai.apiKeyHint')
    if (input.form.platform === 'gemini') return input.t('admin.accounts.gemini.apiKeyHint')
    // Grok 与多协议供应商没有对应的说明文案；通用文案指 Claude Console Key。
    if (input.form.platform === 'grok' || input.isMultiProtocolPlatform.value) return ''
    return input.t('admin.accounts.apiKeyHint')
  })


  // Base URL / API Key 占位符：国产供应商随账号类型变化。
  const apiKeyBaseUrlPlaceholder = computed(() => {
    if (input.isMultiProtocolPlatform.value) {
      const mode = input.form.platform === 'opencode_go' ? input.openCodeAccountMode.value : input.accountMode.value
      return input.defaultCNBaseUrl(input.form.platform, mode, input.apiProtocol.value) || 'https://api.example.com'
    }
    switch (input.form.platform) {
      case 'openai':
        return 'https://api.openai.com'
      case 'gemini':
        return 'https://generativelanguage.googleapis.com'
      case 'grok':
        return 'https://api.x.ai/v1'
      case 'typesafe':
        return 'https://api.typesafe.ai'
      default:
        return 'https://api.anthropic.com'
    }
  })


  const apiKeyValuePlaceholder = computed(() => {
    switch (input.form.platform) {
      case 'openai':
        return 'sk-proj-...'
      case 'gemini':
        return 'AQ.Ab...'
      case 'grok':
        return 'xai-...'
      case 'kimi':
        return 'sk-...'
      case 'zhipu':
        return '<api-key>.<secret>'
      case 'deepseek':
        return 'sk-...'
      case 'minimax':
      case 'opencode_go':
        return 'sk-...'
      case 'typesafe':
        return 'ts-...'
      default:
        return input.isMultiProtocolPlatform.value ? 'sk-...' : 'sk-ant-...'
    }
  })


  // State
  const step = ref(1)

  const accountCategory = ref<'oauth-based' | 'apikey' | 'bedrock' | 'service_account'>('oauth-based')
  // UI selection for account category
  const addMethod = ref<AddMethod>('oauth')

  const apiKeyBaseUrl = ref('https://api.anthropic.com')

  const apiKeyValue = ref('')
  // For antigravity accounts: enable AI Credits overages
  const antigravityAccountType = ref<'oauth' | 'upstream'>('oauth')
  // For antigravity: oauth or upstream
  const antigravityProjectId = ref('')

  const upstreamBaseUrl = ref('')
  // For upstream type: base URL
  const upstreamApiKey = ref('')


  // Bedrock credentials
  const bedrockAuthMode = ref<'sigv4' | 'apikey'>('sigv4')

  const bedrockAccessKeyId = ref('')

  const bedrockSecretAccessKey = ref('')

  const bedrockSessionToken = ref('')

  const bedrockRegion = ref('us-east-1')

  const bedrockForceGlobal = ref(false)

  const bedrockApiKeyValue = ref('')

  const vertexServiceAccountFileInput = ref<HTMLInputElement | null>(null)

  const vertexServiceAccountJson = ref('')

  const vertexProjectId = ref('')

  const vertexClientEmail = ref('')

  const vertexLocation = ref('global')

  const vertexServiceAccountDragActive = ref(false)

  const vertexAuthMode = ref<'service_account' | 'apikey'>('service_account')

  const vertexApiKey = ref('')

  // API Key 认证只对 Gemini 平台开放，Anthropic 平台的 Vertex 账号始终使用 Service Account JSON。
  const vertexUsesApiKey = computed(() => input.form.platform === 'gemini' && vertexAuthMode.value === 'apikey')

  const geminiAIStudioOAuthEnabled = ref(false)

  const showAdvancedOAuth = ref(false)

  const showGeminiHelpDialog = ref(false)


  // Gemini tier selection (used as fallback when auto-detection is unavailable/fails)
  const geminiTierGoogleOne = ref<'google_one_free' | 'google_ai_pro' | 'google_ai_ultra'>('google_one_free')

  const geminiTierGcp = ref<'gcp_standard' | 'gcp_enterprise'>('gcp_standard')

  const geminiTierAIStudio = ref<'aistudio_free' | 'aistudio_paid'>('aistudio_free')


  const geminiSelectedTier = computed(() => {
    if (input.form.platform !== 'gemini') return ''
    if (accountCategory.value === 'apikey') return geminiTierAIStudio.value
    switch (input.geminiOAuthType.value) {
      case 'google_one':
        return geminiTierGoogleOne.value
      case 'code_assist':
        return geminiTierGcp.value
      default:
        return geminiTierAIStudio.value
    }
  })


  const applyVertexServiceAccountJson = (value: string) => {
    const raw = value.trim()
    if (!raw) {
      vertexProjectId.value = ''
      vertexClientEmail.value = ''
      return false
    }
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>
      const projectId = typeof parsed.project_id === 'string' ? parsed.project_id.trim() : ''
      const clientEmail = typeof parsed.client_email === 'string' ? parsed.client_email.trim() : ''
      const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
      if (!projectId || !clientEmail || !privateKey) {
        input.appStore.showError(input.t('admin.accounts.vertexSaJsonMissingFields'))
        return false
      }
      vertexProjectId.value = projectId
      vertexClientEmail.value = clientEmail
      vertexServiceAccountJson.value = JSON.stringify(parsed)
      return true
    } catch {
      input.appStore.showError(input.t('admin.accounts.vertexSaJsonInvalid'))
      return false
    }
  }


  const parseVertexServiceAccountJson = () => applyVertexServiceAccountJson(vertexServiceAccountJson.value)


  const handleVertexServiceAccountFile = async (event: Event) => {
    const pending = input.resourceTasks.capture()
    try {

      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      if (!file) return
      try {
        applyVertexServiceAccountJson(await pending.wait(async () => file.text()))
      } finally {
        if (pending.current()) {

          input.value = ''

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleVertexServiceAccountDrop = async (event: DragEvent) => {
    const pending = input.resourceTasks.capture()
    try {

      vertexServiceAccountDragActive.value = false
      const file = event.dataTransfer?.files?.[0]
      if (!file) return
      applyVertexServiceAccountJson(await pending.wait(async () => file.text()))

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { baseUrlHint, apiKeyHint, apiKeyBaseUrlPlaceholder, apiKeyValuePlaceholder, step, accountCategory, addMethod, apiKeyBaseUrl, apiKeyValue, antigravityAccountType, antigravityProjectId, upstreamBaseUrl, upstreamApiKey, bedrockAuthMode, bedrockAccessKeyId, bedrockSecretAccessKey, bedrockSessionToken, bedrockRegion, bedrockForceGlobal, bedrockApiKeyValue, vertexServiceAccountFileInput, vertexServiceAccountJson, vertexProjectId, vertexClientEmail, vertexLocation, vertexServiceAccountDragActive, vertexAuthMode, vertexApiKey, vertexUsesApiKey, geminiAIStudioOAuthEnabled, showAdvancedOAuth, showGeminiHelpDialog, geminiTierGoogleOne, geminiTierGcp, geminiTierAIStudio, geminiSelectedTier, applyVertexServiceAccountJson, parseVertexServiceAccountJson, handleVertexServiceAccountFile, handleVertexServiceAccountDrop }
}
