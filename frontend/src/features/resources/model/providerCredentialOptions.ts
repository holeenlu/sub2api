import type { ResourceEditorServices } from '../services'
import type { ProviderProtocolRule } from '@/features/catalog'
import type { CnApiProtocol, CnNativeApiProtocol, OpenCodeGoProtocolRule, CNUsageCellAccount } from './credentialsBuilder'






const NATIVE_API_PROTOCOLS: CnNativeApiProtocol[] = ['chat_completions', 'anthropic', 'responses']






function isNativeOpenCodeGoProtocol(value: unknown): value is CnNativeApiProtocol {
  return value === 'chat_completions' || value === 'anthropic' || value === 'responses'
}






/** 从 protocols 列表中取出首选协议以外的原生协议（去重、保持顺序）。 */
function extraRuleProtocols(protocol: CnNativeApiProtocol, protocols: unknown): CnNativeApiProtocol[] {
  if (!Array.isArray(protocols)) return []
  const extra: CnNativeApiProtocol[] = []
  for (const item of protocols) {
    if (isNativeOpenCodeGoProtocol(item) && item !== protocol && !extra.includes(item)) extra.push(item)
  }
  return extra
}






function buildRule(pattern: string, protocol: CnNativeApiProtocol, protocols?: unknown): OpenCodeGoProtocolRule {
  const extra = extraRuleProtocols(protocol, protocols)
  return extra.length > 0 ? { pattern, protocol, extraProtocols: extra } : { pattern, protocol }
}






function normalizeProfileRules(rules: ProviderProtocolRule[] | undefined): OpenCodeGoProtocolRule[] {
  return (rules ?? [])
    .filter(rule => rule.pattern && isNativeOpenCodeGoProtocol(rule.protocol))
    .map(rule => buildRule(rule.pattern, rule.protocol as CnNativeApiProtocol, rule.protocols))
}






// Command Code、Cline 的积分与用量接口只查官方主机上的 API Key 账号（与后端
// commandCodeUsageSupported / clineAccountAPISupported 一致）：自定义中转的 Key 不发往
// 官方接口，后端直接拒绝探测，单元格只会显示一行报错。
const OFFICIAL_ACCOUNT_API_PLATFORMS = ['command_code', 'cline']






function cnUsageCellAccountMode(account: CNUsageCellAccount): string {
  const mode = account.credentials?.account_mode
  return typeof mode === 'string' ? mode : ''
}




export function createProviderCredentialOptions(catalog: ResourceEditorServices['catalog']) {
  const { getProviderMode, getProviderProfile } = catalog
  const BUILTIN_PLATFORM_CATALOG = catalog.builtin


  /** 请求头覆写资格（与后端 IsHeaderOverrideEligible 保持一致） */
  function isHeaderOverrideCapable(platform: string, type: string): boolean {
    if (platform === 'anthropic' || platform === 'openai' || isMultiProtocolApiKeyPlatform(platform)) {
      return type === 'apikey'
    }
    if (platform === 'grok') {
      return type === 'apikey' || type === 'oauth'
    }
    return false
  }


  /** 平台是否为多协议 API Key 供应商（平台清单中带 profile）。 */
  function isMultiProtocolApiKeyPlatform(platform: string): boolean {
    return !!getProviderProfile(platform)
  }


  /** 供应商在该接入模式下是否提供某协议的原生端点（未知模式按默认模式）。 */
  function providerSupportsProtocol(platform: string, mode: string | undefined, protocol: CnNativeApiProtocol): boolean {
    return !!getProviderMode(platform, mode)?.base_urls[protocol]
  }


  /** 供应商是否提供原生 Responses 端点（DeepSeek / Kimi / MiniMax / OpenCode 等）。 */
  function cnSupportsNativeResponses(platform: string, mode?: string): boolean {
    return providerSupportsProtocol(platform, mode, 'responses')
  }


  /** 该接入模式下可直连的原生协议（按 chat_completions、anthropic、responses 顺序）。 */
  function providerNativeProtocols(platform: string, mode?: string): CnNativeApiProtocol[] {
    return NATIVE_API_PROTOCOLS.filter(protocol => providerSupportsProtocol(platform, mode, protocol))
  }


  /** 供应商按模型名选择上游协议（多模型聚合平台），此时 adaptive 账号携带分流规则。 */
  function providerRoutesByModel(platform: string): boolean {
    return getProviderProfile(platform)?.routing === 'by_model'
  }


  /** 上游模型目录给出每个模型支持的协议；未命中分流规则的模型据此选协议。 */
  function providerHasModelCatalog(platform: string): boolean {
    return getProviderProfile(platform)?.model_catalog === true
  }


  /** 供应商的接入模式，默认模式在前。 */
  function providerAccountModes(platform: string): string[] {
    return getProviderProfile(platform)?.modes.map(item => item.mode) ?? []
  }


  /** 规范化接入模式：不在该供应商模式列表中的值回落默认模式。 */
  function resolveProviderAccountMode(platform: string, value: unknown): string {
    const profile = getProviderProfile(platform)
    if (!profile) return typeof value === 'string' ? value : ''
    if (typeof value === 'string' && profile.modes.some(item => item.mode === value)) return value
    return profile.default_mode
  }


  function builtinModeBaseUrl(platform: string, mode: string, protocol: CnNativeApiProtocol): string {
    const profile = BUILTIN_PLATFORM_CATALOG.platforms.find(spec => spec.id === platform)?.multi_protocol
    return profile?.modes.find(item => item.mode === mode)?.base_urls[protocol] ?? ''
  }


  const OPENCODE_GO_BASE_URL = builtinModeBaseUrl('opencode_go', 'go', 'chat_completions')

  const OPENCODE_GO_ANTHROPIC_BASE_URL = builtinModeBaseUrl('opencode_go', 'go', 'anthropic')

  const OPENCODE_ZEN_BASE_URL = builtinModeBaseUrl('opencode_go', 'zen', 'chat_completions')

  const OPENCODE_ZEN_ANTHROPIC_BASE_URL = builtinModeBaseUrl('opencode_go', 'zen', 'anthropic')


  function builtinProtocolRules(platform: string, mode: string): OpenCodeGoProtocolRule[] {
    const profile = BUILTIN_PLATFORM_CATALOG.platforms.find(spec => spec.id === platform)?.multi_protocol
    return normalizeProfileRules(profile?.modes.find(item => item.mode === mode)?.protocol_rules)
  }


  const DEFAULT_OPENCODE_GO_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = builtinProtocolRules('opencode_go', 'go')

  const DEFAULT_OPENCODE_ZEN_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = builtinProtocolRules('opencode_go', 'zen')


  /** 供应商该接入模式的内置分流规则（首条命中生效；未知模式按默认模式）。 */
  function defaultProviderProtocolRules(platform: string, mode?: string): OpenCodeGoProtocolRule[] {
    return normalizeProfileRules(getProviderMode(platform, mode)?.protocol_rules)
  }


  function defaultOpenCodeProtocolRules(mode: string = 'go'): OpenCodeGoProtocolRule[] {
    return defaultProviderProtocolRules('opencode_go', mode === 'zen' ? 'zen' : 'go')
  }


  function cloneOpenCodeGoProtocolRules(
    rules: OpenCodeGoProtocolRule[] = DEFAULT_OPENCODE_GO_PROTOCOL_RULES
  ): OpenCodeGoProtocolRule[] {
    return rules.map(rule => buildRule(rule.pattern, rule.protocol, rule.extraProtocols))
  }


  function parseOpenCodeGoProtocolRules(raw: unknown): OpenCodeGoProtocolRule[] | null {
    if (raw == null) return null
    if (!Array.isArray(raw)) return cloneOpenCodeGoProtocolRules()
    const rules: OpenCodeGoProtocolRule[] = []
    for (const item of raw) {
      if (!item || typeof item !== 'object') continue
      const pattern = typeof (item as { pattern?: unknown }).pattern === 'string'
        ? (item as { pattern: string }).pattern.trim()
        : ''
      const protocols = (item as { protocols?: unknown }).protocols
      let protocol = (item as { protocol?: unknown }).protocol
      // 与后端一致：只给了 protocols 时首项即首选协议。
      if ((protocol == null || protocol === '') && Array.isArray(protocols)) protocol = protocols[0]
      if (!pattern || !isNativeOpenCodeGoProtocol(protocol)) continue
      rules.push(buildRule(pattern, protocol, protocols))
    }
    return rules
  }


  /**
   * 返回指定供应商 + 接入模式 + API 协议的默认 base url（来自平台清单）。
   * 该模式不提供原生 Responses 时回落 Chat Completions 基址；不提供 Anthropic 时为空串。
   */
  function defaultCNBaseUrl(
    platform: string,
    mode: string,
    protocol: CnApiProtocol = 'chat_completions'
  ): string {
    const baseUrls = getProviderMode(platform, mode)?.base_urls
    if (!baseUrls) return ''
    const chatBaseUrl = baseUrls.chat_completions ?? ''
    switch (protocol) {
      case 'anthropic':
        return baseUrls.anthropic ?? ''
      case 'responses':
        return baseUrls.responses || chatBaseUrl
      default:
        return chatBaseUrl
    }
  }


  /** 返回自适应模式下各原生协议的默认端点；不提供的协议为空串。 */
  function defaultCNAdaptiveBaseUrls(
    platform: string,
    mode: string
  ): Record<CnNativeApiProtocol, string> {
    const baseUrls = getProviderMode(platform, mode)?.base_urls ?? {}
    return {
      chat_completions: baseUrls.chat_completions ?? '',
      anthropic: baseUrls.anthropic ?? '',
      responses: baseUrls.responses ?? ''
    }
  }


  /** 与后端 Account.GetAPIProtocol 一致：按模型分流的供应商未指定原生协议时为 adaptive。 */
  function accountUsesAdaptiveProtocol(platform: string, mode: string, protocol: unknown): boolean {
    if (protocol === 'adaptive') return true
    if (protocol === 'chat_completions' || protocol === 'anthropic') return false
    if (protocol === 'responses' && cnSupportsNativeResponses(platform, mode)) return false
    return providerRoutesByModel(platform)
  }


  /**
   * 账号实际使用的 Chat Completions 端点（与后端 Account.GetOpenAIBaseURL 一致）：adaptive
   * 账号优先分协议端点，其次 base_url，都没填时为平台清单的默认端点。
   */
  function accountChatBaseUrl(account: CNUsageCellAccount, mode: string): string {
    const credentials = account.credentials
    const baseUrls = credentials?.api_base_urls
    if (baseUrls && typeof baseUrls === 'object' && accountUsesAdaptiveProtocol(account.platform, mode, credentials?.api_protocol)) {
      const chat = (baseUrls as Record<string, unknown>).chat_completions
      if (typeof chat === 'string' && chat.trim()) return chat.trim()
    }
    const baseUrl = credentials?.base_url
    if (typeof baseUrl === 'string' && baseUrl.trim()) return baseUrl.trim()
    return defaultCNBaseUrl(account.platform, mode)
  }


  /** 官方主机即平台清单默认端点的主机，且必须是 https。 */
  function officialAccountAPISupported(account: CNUsageCellAccount, mode: string): boolean {
    if (account.type !== 'apikey') return false
    try {
      const target = new URL(accountChatBaseUrl(account, mode))
      const official = new URL(defaultCNBaseUrl(account.platform, mode))
      return target.protocol === 'https:' && target.hostname === official.hostname
    } catch {
      return false
    }
  }


  function cnQuotaCellVisible(account: CNUsageCellAccount): boolean {
    const { platform } = account
    const mode = cnUsageCellAccountMode(account)
    if (platform === 'opencode_go') return mode !== 'zen'
    // Command Code、Cline：订阅窗口与积分由一次探测同时刷新。
    if (OFFICIAL_ACCOUNT_API_PLATFORMS.includes(platform)) return officialAccountAPISupported(account, mode)
    return (platform === 'kimi' || platform === 'zhipu' || platform === 'minimax') && mode === 'coding'
  }


  function cnBalanceCellVisible(account: CNUsageCellAccount): boolean {
    const { platform } = account
    const mode = cnUsageCellAccountMode(account)
    if (OFFICIAL_ACCOUNT_API_PLATFORMS.includes(platform)) return officialAccountAPISupported(account, mode)
    return (platform === 'kimi' || platform === 'deepseek') && mode !== 'coding'
  }
  return { isHeaderOverrideCapable, isMultiProtocolApiKeyPlatform, providerSupportsProtocol, cnSupportsNativeResponses, providerNativeProtocols, providerRoutesByModel, providerHasModelCatalog, providerAccountModes, resolveProviderAccountMode, OPENCODE_GO_BASE_URL, OPENCODE_GO_ANTHROPIC_BASE_URL, OPENCODE_ZEN_BASE_URL, OPENCODE_ZEN_ANTHROPIC_BASE_URL, DEFAULT_OPENCODE_GO_PROTOCOL_RULES, DEFAULT_OPENCODE_ZEN_PROTOCOL_RULES, defaultProviderProtocolRules, defaultOpenCodeProtocolRules, cloneOpenCodeGoProtocolRules, parseOpenCodeGoProtocolRules, defaultCNBaseUrl, defaultCNAdaptiveBaseUrls, cnQuotaCellVisible, cnBalanceCellVisible }
}
