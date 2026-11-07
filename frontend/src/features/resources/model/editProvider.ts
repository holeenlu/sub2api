import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed } from 'vue'
import { isCNProviderPlatform, type CnAccountMode, type CnApiProtocol, type CnNativeApiProtocol, type CnProviderPlatform, type OpenCodeAccountMode, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import type { Ref, ComputedRef } from 'vue'
import type { Props } from './editEditorTypes'




interface EditProviderInput {
  props: Readonly<Pick<Props, "account">>
  isMultiProtocolApiKeyPlatform: ReturnType<typeof createProviderCredentialOptions>["isMultiProtocolApiKeyPlatform"]
  providerAccountModes: ReturnType<typeof createProviderCredentialOptions>["providerAccountModes"]
  cloneOpenCodeGoProtocolRules: ReturnType<typeof createProviderCredentialOptions>["cloneOpenCodeGoProtocolRules"]
  providerNativeProtocols: ReturnType<typeof createProviderCredentialOptions>["providerNativeProtocols"]
}




export function useEditProviderDraft(input: EditProviderInput): {
  isCNApiKeyAccount: ComputedRef<boolean>
  isGenericMultiProtocolAccount: ComputedRef<boolean>
  genericAccountModes: ComputedRef<string[]>
  cnPresetPlatform: ComputedRef<CnProviderPlatform>
  adaptivePresetPlatform: ComputedRef<string>
  editApiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  editOpenCodeGoProtocolRules: Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>
  editAccountMode: Ref<string, string>
  editOpenCodeAccountMode: Ref<OpenCodeAccountMode, OpenCodeAccountMode>
  cnPresetMode: ComputedRef<CnAccountMode>
  currentOpenCodeOrCNMode: () => string
  editZhipuOrganization: Ref<string, string>
  editZhipuProject: Ref<string, string>
  editAdaptiveBaseUrls: Ref<{ chat_completions: string; anthropic: string; responses: string }, Record<CnNativeApiProtocol, string> | { chat_completions: string; anthropic: string; responses: string }>
  cnAccountModeOptions: ComputedRef<{ value: CnAccountMode; labelKey: "payg" | "coding" }[]>
  NATIVE_PROTOCOL_LABEL_KEYS: Record<CnNativeApiProtocol, string>
  editAdaptiveProtocolOptions: ComputedRef<{ value: CnNativeApiProtocol; labelKey: string }[]>
  cnProtocolOptions: ComputedRef<{ value: CnApiProtocol; labelKey: string }[]>
  cnProtocolDescKey: ComputedRef<string>
} {


  // ── 国产供应商（Kimi / Zhipu / DeepSeek）account_mode / api_protocol 编辑 ──
  // account_mode 决定额度/余额监控路径，api_protocol 决定转发端点与格式；
  // 二者均可修正（早期创建的账号可能存错默认值），切换时重置 base_url 预置。
  // 覆盖全部多协议 API Key 供应商（国产厂商、OpenCode 与走通用表单的供应商）。
  const isCNApiKeyAccount = computed(
    () => input.props.account?.type === 'apikey' && input.isMultiProtocolApiKeyPlatform(input.props.account.platform)
  )

  // 前端没有专属界面、走通用表单的多协议供应商：模式 / 协议 / 默认端点来自 profile。
  const isGenericMultiProtocolAccount = computed(
    () =>
      isCNApiKeyAccount.value &&
      !isCNProviderPlatform(input.props.account?.platform ?? '') &&
      input.props.account?.platform !== 'opencode_go'
  )

  const genericAccountModes = computed(() => input.providerAccountModes(input.props.account?.platform ?? ''))

  // CnBaseUrlPresets 的 platform prop 是平台字面量联合类型，模板里不能写
  // `as` 断言（其中的 `|` 会被 eslint 误判为 Vue2 filter 语法），经此 computed 传递。
  const cnPresetPlatform = computed<CnProviderPlatform>(() => {
    const platform = input.props.account?.platform
    if (isCNProviderPlatform(platform ?? '')) {
      return platform as CnProviderPlatform
    }
    return 'kimi'
  })

  const adaptivePresetPlatform = computed<string>(() => {
    if (isCNApiKeyAccount.value) return input.props.account!.platform
    return cnPresetPlatform.value
  })

  const editApiProtocol = ref<CnApiProtocol>('adaptive')

  const editOpenCodeGoProtocolRules = ref<OpenCodeGoProtocolRule[]>(input.cloneOpenCodeGoProtocolRules())

  // 多协议供应商（国产厂商与走通用表单的供应商）的接入模式；OpenCode 用 editOpenCodeAccountMode。
  const editAccountMode = ref<string>('payg')

  const editOpenCodeAccountMode = ref<OpenCodeAccountMode>('go')

  // 国产厂商的接入模式只有 payg / coding。
  const cnPresetMode = computed<CnAccountMode>(() => (editAccountMode.value === 'coding' ? 'coding' : 'payg'))

  function currentOpenCodeOrCNMode(): string {
    return input.props.account?.platform === 'opencode_go' ? editOpenCodeAccountMode.value : editAccountMode.value
  }

  // 智谱团队版 Coding Plan：组织/项目 ID，写入 credentials 供额度探测切换团队端点
  const editZhipuOrganization = ref('')

  const editZhipuProject = ref('')

  const editAdaptiveBaseUrls = ref<Record<CnNativeApiProtocol, string>>({
    chat_completions: '',
    anthropic: '',
    responses: ''
  })

  const cnAccountModeOptions = computed<Array<{ value: CnAccountMode; labelKey: 'payg' | 'coding' }>>(
    () => {
      // DeepSeek 无 coding 套餐（与创建弹窗一致），仅保留按量付费。
      if (input.props.account?.platform === 'deepseek') {
        return [{ value: 'payg', labelKey: 'payg' }]
      }
      return [
        { value: 'payg', labelKey: 'payg' },
        { value: 'coding', labelKey: 'coding' }
      ]
    }
  )

  const NATIVE_PROTOCOL_LABEL_KEYS: Record<CnNativeApiProtocol, string> = {
    chat_completions: 'chatCompletions',
    anthropic: 'anthropic',
    responses: 'responses'
  }

  // 当前供应商与接入模式提供原生端点的协议（profile 中有默认基址的协议）。
  const editAdaptiveProtocolOptions = computed<Array<{ value: CnNativeApiProtocol; labelKey: string }>>(() =>
    input.providerNativeProtocols(input.props.account?.platform ?? '', currentOpenCodeOrCNMode()).map(value => ({
      value,
      labelKey: NATIVE_PROTOCOL_LABEL_KEYS[value]
    }))
  )

  const cnProtocolOptions = computed<Array<{ value: CnApiProtocol; labelKey: string }>>(() => [
    { value: 'adaptive', labelKey: 'adaptive' },
    ...editAdaptiveProtocolOptions.value
  ])

  const cnProtocolDescKey = computed(
    () => cnProtocolOptions.value.find(o => o.value === editApiProtocol.value)?.labelKey ?? 'chatCompletions'
  )
  return { isCNApiKeyAccount, isGenericMultiProtocolAccount, genericAccountModes, cnPresetPlatform, adaptivePresetPlatform, editApiProtocol, editOpenCodeGoProtocolRules, editAccountMode, editOpenCodeAccountMode, cnPresetMode, currentOpenCodeOrCNMode, editZhipuOrganization, editZhipuProject, editAdaptiveBaseUrls, cnAccountModeOptions, NATIVE_PROTOCOL_LABEL_KEYS, editAdaptiveProtocolOptions, cnProtocolOptions, cnProtocolDescKey }
}
