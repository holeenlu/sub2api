import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type CnApiProtocol } from "@/features/resources/model/credentialsBuilder"
import type { Ref, ComputedRef } from 'vue'
import type { Props } from './editEditorTypes'




interface EditCredentialsInput {
  props: Readonly<Pick<Props, "account">>
  t: ReturnType<typeof useI18n>["t"]
  isMultiProtocolApiKeyPlatform: ReturnType<typeof createProviderCredentialOptions>["isMultiProtocolApiKeyPlatform"]
  defaultCNBaseUrl: ReturnType<typeof createProviderCredentialOptions>["defaultCNBaseUrl"]
  currentOpenCodeOrCNMode: () => string
  editApiProtocol: Ref<CnApiProtocol, CnApiProtocol>
}




export function useEditCredentialsDraft(input: EditCredentialsInput): {
  baseUrlHint: ComputedRef<string>
  editBaseUrl: Ref<string, string>
  editApiKey: Ref<string, string>
  editBedrockAccessKeyId: Ref<string, string>
  editBedrockSecretAccessKey: Ref<string, string>
  editBedrockSessionToken: Ref<string, string>
  editBedrockRegion: Ref<string, string>
  editBedrockForceGlobal: Ref<boolean, boolean>
  editBedrockApiKeyValue: Ref<string, string>
  editVertexProjectId: Ref<string, string>
  editVertexClientEmail: Ref<string, string>
  editVertexLocation: Ref<string, string>
  editVertexApiKey: Ref<string, string>
  isVertexAPIKeyMode: ComputedRef<boolean>
  isBedrockAPIKeyMode: ComputedRef<boolean>
  antigravityProjectId: Ref<string, string>
  defaultBaseUrl: ComputedRef<string>
} {


  // Platform-specific hint for Base URL
  const baseUrlHint = computed(() => {
    if (!input.props.account) return input.t('admin.accounts.baseUrlHint')
    if (input.props.account.platform === 'openai') return input.t('admin.accounts.openai.baseUrlHint')
    if (input.props.account.platform === 'gemini') return input.t('admin.accounts.gemini.baseUrlHint')
    if (input.props.account.platform === 'grok') return ''
    return input.t('admin.accounts.baseUrlHint')
  })

  const editBaseUrl = ref('https://api.anthropic.com')

  const editApiKey = ref('')

  // Bedrock credentials
  const editBedrockAccessKeyId = ref('')

  const editBedrockSecretAccessKey = ref('')

  const editBedrockSessionToken = ref('')

  const editBedrockRegion = ref('')

  const editBedrockForceGlobal = ref(false)

  const editBedrockApiKeyValue = ref('')

  const editVertexProjectId = ref('')

  const editVertexClientEmail = ref('')

  const editVertexLocation = ref('us-central1')

  const editVertexApiKey = ref('')

  const isVertexAPIKeyMode = computed(() =>
    input.props.account?.platform === 'gemini' &&
    input.props.account?.type === 'service_account' &&
    (input.props.account?.credentials as Record<string, unknown>)?.auth_mode === 'apikey'
  )

  const isBedrockAPIKeyMode = computed(() =>
    input.props.account?.type === 'bedrock' &&
    (input.props.account?.credentials as Record<string, unknown>)?.auth_mode === 'apikey'
  )
  // For antigravity accounts: enable AI Credits overages
  const antigravityProjectId = ref('')


  // Computed: default base URL based on platform
  const defaultBaseUrl = computed(() => {
    if (input.props.account?.platform === 'openai') return 'https://api.openai.com'
    if (input.props.account?.platform === 'gemini') return 'https://generativelanguage.googleapis.com'
    if (input.props.account?.platform === 'grok') return 'https://api.x.ai/v1'
    if (input.props.account?.platform === 'typesafe') return 'https://api.typesafe.ai'
    // CN 供应商：按当前模式/协议回落到官方预设（清空输入框提交时使用），
    // 不能落到 anthropic 默认值（会被当 CC base 拼出错误端点）。
    if (input.props.account && input.isMultiProtocolApiKeyPlatform(input.props.account.platform)) {
      return input.defaultCNBaseUrl(input.props.account.platform, input.currentOpenCodeOrCNMode(), input.editApiProtocol.value)
    }
    return 'https://api.anthropic.com'
  })
  return { baseUrlHint, editBaseUrl, editApiKey, editBedrockAccessKeyId, editBedrockSecretAccessKey, editBedrockSessionToken, editBedrockRegion, editBedrockForceGlobal, editBedrockApiKeyValue, editVertexProjectId, editVertexClientEmail, editVertexLocation, editVertexApiKey, isVertexAPIKeyMode, isBedrockAPIKeyMode, antigravityProjectId, defaultBaseUrl }
}
