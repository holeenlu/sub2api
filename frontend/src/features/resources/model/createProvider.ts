import type { CreateResourceForm } from './createEditorTypes'
import type { PlatformSpec } from '@/features/catalog'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed } from 'vue'
import { isCNProviderPlatform, type CnAccountMode, type CnApiProtocol, type CnNativeApiProtocol, type CnProviderPlatform, type OpenCodeAccountMode, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import type { Ref, ComputedRef } from 'vue'




interface CreateProviderInput {
  cloneOpenCodeGoProtocolRules: ReturnType<typeof createProviderCredentialOptions>["cloneOpenCodeGoProtocolRules"]
  defaultOpenCodeProtocolRules: ReturnType<typeof createProviderCredentialOptions>["defaultOpenCodeProtocolRules"]
  form: Pick<CreateResourceForm, "platform" | "type">
  isMultiProtocolApiKeyPlatform: ReturnType<typeof createProviderCredentialOptions>["isMultiProtocolApiKeyPlatform"]
  isOpenCodeGoPlatform: ComputedRef<boolean>
  listPlatforms: () => PlatformSpec[]
  providerAccountModes: ReturnType<typeof createProviderCredentialOptions>["providerAccountModes"]
  providerNativeProtocols: ReturnType<typeof createProviderCredentialOptions>["providerNativeProtocols"]
  defaultCNAdaptiveBaseUrls: ReturnType<typeof createProviderCredentialOptions>["defaultCNAdaptiveBaseUrls"]
  accountCategory: Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">
  resolveProviderAccountMode: ReturnType<typeof createProviderCredentialOptions>["resolveProviderAccountMode"]
  apiKeyBaseUrl: Ref<string, string>
  defaultCNBaseUrl: ReturnType<typeof createProviderCredentialOptions>["defaultCNBaseUrl"]
  defaultProviderProtocolRules: ReturnType<typeof createProviderCredentialOptions>["defaultProviderProtocolRules"]
}




export function useCreateProviderDraft(input: CreateProviderInput): {
  accountMode: Ref<string, string>
  openCodeAccountMode: Ref<OpenCodeAccountMode, OpenCodeAccountMode>
  apiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  openCodeGoProtocolRules: Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>
  zhipuOrganization: Ref<string, string>
  zhipuProject: Ref<string, string>
  adaptiveBaseUrls: Ref<{ anthropic: string; chat_completions: string; responses: string }, Record<CnNativeApiProtocol, string> | { anthropic: string; chat_completions: string; responses: string }>
  isCNPlatform: ComputedRef<boolean>
  isMultiProtocolPlatform: ComputedRef<boolean>
  isGenericMultiProtocolPlatform: ComputedRef<boolean>
  extraMultiProtocolPlatforms: ComputedRef<PlatformSpec[]>
  genericAccountModes: ComputedRef<string[]>
  currentOpenCodeOrCNMode: () => string
  cnPresetPlatform: ComputedRef<CnProviderPlatform>
  cnPresetMode: ComputedRef<CnAccountMode>
  adaptivePresetPlatform: ComputedRef<string>
  NATIVE_PROTOCOL_LABEL_KEYS: Record<CnNativeApiProtocol, string>
  cnAdaptiveProtocolOptions: ComputedRef<{ value: CnNativeApiProtocol; labelKey: string }[]>
  cnProtocolOptions: ComputedRef<{ value: CnApiProtocol; labelKey: string }[]>
  resetAdaptiveBaseUrls: (platform: string, mode: string) => void
  cnAccentActiveClass: ComputedRef<"border-pink-500 bg-pink-50 dark:bg-pink-900/20" | "border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20" | "border-teal-500 bg-teal-50 dark:bg-teal-900/20" | "border-rose-500 bg-rose-50 dark:bg-rose-900/20" | "border-amber-500 bg-amber-50 dark:bg-amber-900/20" | "border-primary-500 bg-primary-50 dark:bg-primary-900/20">
  cnAccentIconClass: ComputedRef<"bg-pink-500 text-white" | "bg-indigo-500 text-white" | "bg-teal-500 text-white" | "bg-rose-500 text-white" | "bg-amber-500 text-white" | "bg-primary-500 text-white">
  selectCNPlatform: (platform: CnProviderPlatform) => void
  selectOpenCodeGoPlatform: () => void
  selectTypeSafePlatform: () => void
  selectGenericMultiProtocolPlatform: (platform: string) => void
} {


  // ── 国产供应商（Kimi / Zhipu / DeepSeek）账号类型、API 协议与端点 ──
  // 多协议供应商（国产厂商与走通用表单的供应商）的接入模式；OpenCode 用 openCodeAccountMode。
  const accountMode = ref<string>('payg')

  const openCodeAccountMode = ref<OpenCodeAccountMode>('zen')

  // API 协议决定转发端点与格式：cc=现有转换链，anthropic=原生直通（Claude Code），
  // responses=deepseek / kimi 原生 Responses 端点（Codex）。与账号类型正交。
  const apiProtocol = ref<CnApiProtocol>('adaptive')

  const openCodeGoProtocolRules = ref<OpenCodeGoProtocolRule[]>(
    input.cloneOpenCodeGoProtocolRules(input.defaultOpenCodeProtocolRules('zen'))
  )

  // 智谱团队版 Coding Plan：组织/项目 ID，写入 credentials 供额度探测切换团队端点
  const zhipuOrganization = ref('')

  const zhipuProject = ref('')

  const adaptiveBaseUrls = ref<Record<CnNativeApiProtocol, string>>({
    chat_completions: '',
    anthropic: '',
    responses: ''
  })

  const isCNPlatform = computed(() => isCNProviderPlatform(input.form.platform))

  const isMultiProtocolPlatform = computed(() => input.isMultiProtocolApiKeyPlatform(input.form.platform))

  // 前端没有专属界面、走通用表单的多协议供应商：模式、协议、默认端点
  // 与分流规则全部来自平台清单中的 profile。
  const isGenericMultiProtocolPlatform = computed(
    () => isMultiProtocolPlatform.value && !isCNPlatform.value && !input.isOpenCodeGoPlatform.value
  )

  const extraMultiProtocolPlatforms = computed(() =>
    input.listPlatforms().filter(spec => !!spec.multi_protocol && !isCNProviderPlatform(spec.id) && spec.id !== 'opencode_go')
  )

  const genericAccountModes = computed(() => input.providerAccountModes(input.form.platform))

  function currentOpenCodeOrCNMode(): string {
    return input.isOpenCodeGoPlatform.value ? openCodeAccountMode.value : accountMode.value
  }

  // CnBaseUrlPresets 的 platform prop 是平台字面量联合类型，模板里不能写
  // `as` 断言（其中的 `|` 会被 eslint 误判为 Vue2 filter 语法），经此 computed 传递。
  const cnPresetPlatform = computed<CnProviderPlatform>(() => {
    if (isCNProviderPlatform(input.form.platform)) {
      return input.form.platform
    }
    return 'kimi'
  })

  // 国产厂商的接入模式只有 payg / coding（selectCNPlatform 已按 profile 规范化）。
  const cnPresetMode = computed<CnAccountMode>(() => (accountMode.value === 'coding' ? 'coding' : 'payg'))

  const adaptivePresetPlatform = computed<string>(() => {
    if (isMultiProtocolPlatform.value) return input.form.platform
    return cnPresetPlatform.value
  })

  const NATIVE_PROTOCOL_LABEL_KEYS: Record<CnNativeApiProtocol, string> = {
    chat_completions: 'chatCompletions',
    anthropic: 'anthropic',
    responses: 'responses'
  }

  // 当前平台与接入模式提供原生端点的协议（profile 中有默认基址的协议）。
  const cnAdaptiveProtocolOptions = computed<Array<{ value: CnNativeApiProtocol; labelKey: string }>>(() =>
    input.providerNativeProtocols(input.form.platform, currentOpenCodeOrCNMode()).map(value => ({
      value,
      labelKey: NATIVE_PROTOCOL_LABEL_KEYS[value]
    }))
  )

  // 可选的协议档：adaptive 加上各原生协议（responses 仅 deepseek / kimi 等提供原生端点的供应商）。
  const cnProtocolOptions = computed<Array<{ value: CnApiProtocol; labelKey: string }>>(() => [
    { value: 'adaptive', labelKey: 'adaptive' },
    ...cnAdaptiveProtocolOptions.value
  ])


  function resetAdaptiveBaseUrls(platform: string, mode: string) {
    adaptiveBaseUrls.value = input.defaultCNAdaptiveBaseUrls(platform, mode)
  }

  // 当前选中平台的品牌色（选中卡片描边 / 图标底色），与 platformColors 取色一致。
  const cnAccentActiveClass = computed(() => {
    switch (input.form.platform) {
      case 'kimi':
        return 'border-pink-500 bg-pink-50 dark:bg-pink-900/20'
      case 'zhipu':
        return 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20'
      case 'deepseek':
        return 'border-teal-500 bg-teal-50 dark:bg-teal-900/20'
      case 'minimax':
        return 'border-rose-500 bg-rose-50 dark:bg-rose-900/20'
      case 'opencode_go':
        return 'border-amber-500 bg-amber-50 dark:bg-amber-900/20'
      default:
        return 'border-primary-500 bg-primary-50 dark:bg-primary-900/20'
    }
  })

  const cnAccentIconClass = computed(() => {
    switch (input.form.platform) {
      case 'kimi':
        return 'bg-pink-500 text-white'
      case 'zhipu':
        return 'bg-indigo-500 text-white'
      case 'deepseek':
        return 'bg-teal-500 text-white'
      case 'minimax':
        return 'bg-rose-500 text-white'
      case 'opencode_go':
        return 'bg-amber-500 text-white'
      default:
        return 'bg-primary-500 text-white'
    }
  })

  // 切换国产供应商平台：强制 apikey 类型，deepseek 无 coding 套餐故锁定 payg，
  // 协议回落 adaptive，并把 base url 重置为该平台默认端点。
  function selectCNPlatform(platform: CnProviderPlatform) {
    input.form.platform = platform
    input.form.type = 'apikey'
    input.accountCategory.value = 'apikey'
    apiProtocol.value = 'adaptive'
    // deepseek 无 coding 套餐（profile 仅 payg）；从其他供应商切换来的未知模式回落默认模式。
    accountMode.value = input.resolveProviderAccountMode(platform, accountMode.value)
    input.apiKeyBaseUrl.value = input.defaultCNBaseUrl(platform, accountMode.value, apiProtocol.value)
    resetAdaptiveBaseUrls(platform, accountMode.value)
  }

  function selectOpenCodeGoPlatform() {
    input.form.platform = 'opencode_go'
    input.form.type = 'apikey'
    input.accountCategory.value = 'apikey'
    apiProtocol.value = 'adaptive'
    openCodeAccountMode.value = 'zen'
    input.apiKeyBaseUrl.value = input.defaultCNBaseUrl('opencode_go', openCodeAccountMode.value, 'adaptive')
    resetAdaptiveBaseUrls('opencode_go', openCodeAccountMode.value)
    openCodeGoProtocolRules.value = input.cloneOpenCodeGoProtocolRules(input.defaultOpenCodeProtocolRules(openCodeAccountMode.value))
  }

  function selectTypeSafePlatform() {
    input.form.platform = 'typesafe'
    input.form.type = 'apikey'
    input.accountCategory.value = 'apikey'
    input.apiKeyBaseUrl.value = 'https://api.typesafe.ai'
  }

  function selectGenericMultiProtocolPlatform(platform: string) {
    input.form.platform = platform
    input.form.type = 'apikey'
    input.accountCategory.value = 'apikey'
    apiProtocol.value = 'adaptive'
    accountMode.value = input.resolveProviderAccountMode(platform, undefined)
    input.apiKeyBaseUrl.value = input.defaultCNBaseUrl(platform, accountMode.value, apiProtocol.value)
    resetAdaptiveBaseUrls(platform, accountMode.value)
    openCodeGoProtocolRules.value = input.cloneOpenCodeGoProtocolRules(input.defaultProviderProtocolRules(platform, accountMode.value))
  }
  return { accountMode, openCodeAccountMode, apiProtocol, openCodeGoProtocolRules, zhipuOrganization, zhipuProject, adaptiveBaseUrls, isCNPlatform, isMultiProtocolPlatform, isGenericMultiProtocolPlatform, extraMultiProtocolPlatforms, genericAccountModes, currentOpenCodeOrCNMode, cnPresetPlatform, cnPresetMode, adaptivePresetPlatform, NATIVE_PROTOCOL_LABEL_KEYS, cnAdaptiveProtocolOptions, cnProtocolOptions, resetAdaptiveBaseUrls, cnAccentActiveClass, cnAccentIconClass, selectCNPlatform, selectOpenCodeGoPlatform, selectTypeSafePlatform, selectGenericMultiProtocolPlatform }
}
