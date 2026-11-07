import type { GroupImageCountBilling, VideoModelPrices } from '@/features/pricing/api/mediaPricingTypes'
import type { ChannelModelPricing } from '@/features/pricing/api/types'
import type { AccountPlatform } from '@/features/resources/api/platformTypes'
import type { SubscriptionType } from '@/features/subscriptions/api/subscriptionPolicyTypes'

export type GroupPlatform = AccountPlatform | 'composite'

export interface OpenAIMessagesDispatchModelConfig {
  opus_mapped_model?: string
  sonnet_mapped_model?: string
  haiku_mapped_model?: string
  exact_model_mappings?: Record<string, string>
}

export type ReasoningEffortMatchType = 'exact' | 'prefix' | 'suffix'

export interface ReasoningEffortMapping {
  from: string
  to: string
  match_type?: ReasoningEffortMatchType
  model?: string
}

export interface GroupLocaleText {
  name?: string
  description?: string
}

export type GroupI18n = Record<string, GroupLocaleText>

export interface Group extends GroupImageCountBilling {
  id: number
  name: string
  description: string | null
  platform: GroupPlatform
  rate_multiplier: number
  rpm_limit?: number // Group-level RPM cap (0 = unlimited); overrides user-level rpm_limit when set
  max_reasoning_effort?: string // Anthropic/OpenAI reasoning ceiling; empty means unlimited
  max_reasoning_effort_over_limit?: string // downgrade (default) or deny when over the ceiling
  reasoning_effort_mappings?: ReasoningEffortMapping[]
  is_exclusive: boolean
  status: 'active' | 'inactive'
  subscription_type: SubscriptionType
  daily_limit_usd: number | null
  weekly_limit_usd: number | null
  monthly_limit_usd: number | null
  long_context_pricing_enabled: boolean
  // 图片生成计费配置
  allow_image_generation: boolean
  allow_batch_image_generation: boolean
  image_rate_independent: boolean
  image_rate_multiplier: number
  batch_image_discount_multiplier: number
  batch_image_hold_multiplier: number
  image_price_1k: number | null
  image_price_2k: number | null
  image_price_4k: number | null
  video_rate_independent: boolean
  video_rate_multiplier: number
  video_price_480p: number | null
  video_price_720p: number | null
  video_price_1080p: number | null
  // Optional model-family x resolution overrides for Grok video pricing.
  video_model_prices?: VideoModelPrices
  // Codex 网页搜索单次价格（USD/次）；null 表示使用默认价 0.01
  web_search_price_per_call: number | null
  // Grok Voice 显式定价（分组级）
  search_price_per_1k: number | null
  audio_realtime_price_per_min: number | null
  audio_tts_price_per_million_chars: number | null
  audio_stt_price_per_hour: number | null
  // 高峰时段倍率配置
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  // Claude Code 客户端限制
  claude_code_only: boolean
  fallback_group_id: number | null
  fallback_group_id_on_invalid_request: number | null
  // OpenAI Messages 调度开关（用户侧需要此字段判断是否展示 Claude Code 教程）
  allow_messages_dispatch?: boolean
  // OpenAI Live 接口开关
  allow_live: boolean
  default_mapped_model?: string
  messages_dispatch_model_config?: OpenAIMessagesDispatchModelConfig
  require_oauth_only: boolean
  require_privacy_set: boolean
  created_at: string
  updated_at: string
}

export interface AdminGroup extends Group {
  // 管理端的 name / description 始终是原字段，各语言译文另放在 i18n 里
  i18n?: GroupI18n
  force_openai_fast: boolean
  free_openai_fast: boolean
  model_pricing: ChannelModelPricing[]
  // 分组利润控制（openai/anthropic/gemini/grok/antigravity 分组可启用；margin/buffer 为小数存储）。
  // 仅管理员可见：与 rate_multiplier 相乘即可反推上游成本上限，不得下放到 Group。
  profit_control_enabled: boolean
  profit_min_margin: number
  profit_safety_buffer: number

  // 模型路由配置（仅管理员可见，内部信息）
  model_routing: Record<string, number[]> | null
  model_routing_enabled: boolean

  // MCP XML 协议注入（仅 antigravity 平台使用）
  mcp_xml_inject: boolean

  // 支持的模型系列（仅 antigravity 平台使用）
  supported_model_scopes?: string[]

  // 分组下账号数量（仅管理员可见）
  account_count?: number
  active_account_count?: number
  rate_limited_account_count?: number

  // OpenAI Messages 调度配置（仅 openai 平台使用）
  default_mapped_model?: string
  messages_dispatch_model_config?: OpenAIMessagesDispatchModelConfig
  model_allowlist?: ModelAllowlist
  codex_models_manifest_config?: CodexModelsManifestConfig

  // 分组排序
  sort_order: number
}

export interface ModelAllowlist {
  enabled: boolean
  models: string[]
}

export interface CodexModelsManifestConfig {
  enabled: boolean
  account_ids: number[]
  fallback_to_scheduler: boolean
}

export type CompositeRouteMatchType = 'exact' | 'prefix'

export type CompositeRouteEndpoint =
  | 'any'
  | 'messages'
  | 'count_tokens'
  | 'responses'
  | 'chat_completions'
  | 'embeddings'
  | 'images'
  | 'gemini'

export type CompositeRouteSource = 'route' | 'detector' | string

export interface CompositeModelRoute {
  id: number
  group_id: number
  public_model: string
  match_type: CompositeRouteMatchType
  target_platform: Exclude<GroupPlatform, 'composite'>
  upstream_model: string
  endpoint: CompositeRouteEndpoint
  priority: number
  enabled: boolean
  notes: string
  created_at?: string
  updated_at?: string
}

export interface CompositeModelRouteInput {
  public_model: string
  match_type: CompositeRouteMatchType
  target_platform: Exclude<GroupPlatform, 'composite'>
  upstream_model?: string
  endpoint: CompositeRouteEndpoint
  priority?: number
  enabled?: boolean
  notes?: string
}

export interface CompositeRoutePreviewRequest {
  model: string
  endpoint: CompositeRouteEndpoint
}

export interface CompositeRouteDecision {
  matched: boolean
  source: CompositeRouteSource
  group_id: number
  public_model: string
  target_platform: Exclude<GroupPlatform, 'composite'> | ''
  upstream_model: string
  endpoint: CompositeRouteEndpoint
  route?: CompositeModelRoute
  reason?: string
}

export interface CreateGroupRequest extends Partial<GroupImageCountBilling> {
  name: string
  description?: string | null
  i18n?: GroupI18n
  platform?: GroupPlatform
  rate_multiplier?: number
  is_exclusive?: boolean
  subscription_type?: SubscriptionType
  daily_limit_usd?: number | null
  weekly_limit_usd?: number | null
  monthly_limit_usd?: number | null
  long_context_pricing_enabled?: boolean
  force_openai_fast?: boolean
  free_openai_fast?: boolean
  model_pricing?: ChannelModelPricing[]
  allow_image_generation?: boolean
  allow_batch_image_generation?: boolean
  image_rate_independent?: boolean
  image_rate_multiplier?: number
  batch_image_discount_multiplier?: number
  batch_image_hold_multiplier?: number
  image_price_1k?: number | null
  image_price_2k?: number | null
  image_price_4k?: number | null
  video_rate_independent?: boolean
  video_rate_multiplier?: number
  video_price_480p?: number | null
  video_price_720p?: number | null
  video_price_1080p?: number | null
  video_model_prices?: VideoModelPrices
  web_search_price_per_call?: number | null
  search_price_per_1k?: number | null
  audio_realtime_price_per_min?: number | null
  audio_tts_price_per_million_chars?: number | null
  audio_stt_price_per_hour?: number | null
  peak_rate_enabled?: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
  // 分组利润控制（五个 token 平台；margin/buffer 为小数）
  profit_control_enabled?: boolean
  profit_min_margin?: number
  profit_safety_buffer?: number
  claude_code_only?: boolean
  fallback_group_id?: number | null
  fallback_group_id_on_invalid_request?: number | null
  mcp_xml_inject?: boolean
  supported_model_scopes?: string[]
  model_allowlist?: ModelAllowlist
  codex_models_manifest_config?: CodexModelsManifestConfig
  allow_messages_dispatch?: boolean
  allow_live?: boolean
  default_mapped_model?: string
  messages_dispatch_model_config?: OpenAIMessagesDispatchModelConfig
  model_routing?: Record<string, number[]> | null
  model_routing_enabled?: boolean
  rpm_limit?: number
  max_reasoning_effort?: string
  max_reasoning_effort_over_limit?: string
  reasoning_effort_mappings?: ReasoningEffortMapping[]
  require_oauth_only?: boolean
  require_privacy_set?: boolean
  // 从指定分组复制账号
  copy_accounts_from_group_ids?: number[]
}

export interface UpdateGroupRequest extends Partial<GroupImageCountBilling> {
  name?: string
  description?: string | null
  i18n?: GroupI18n
  platform?: GroupPlatform
  rate_multiplier?: number
  is_exclusive?: boolean
  status?: 'active' | 'inactive'
  subscription_type?: SubscriptionType
  daily_limit_usd?: number | null
  weekly_limit_usd?: number | null
  monthly_limit_usd?: number | null
  long_context_pricing_enabled?: boolean
  force_openai_fast?: boolean
  free_openai_fast?: boolean
  model_pricing?: ChannelModelPricing[]
  allow_image_generation?: boolean
  allow_batch_image_generation?: boolean
  image_rate_independent?: boolean
  image_rate_multiplier?: number
  batch_image_discount_multiplier?: number
  batch_image_hold_multiplier?: number
  image_price_1k?: number | null
  image_price_2k?: number | null
  image_price_4k?: number | null
  video_rate_independent?: boolean
  video_rate_multiplier?: number
  video_price_480p?: number | null
  video_price_720p?: number | null
  video_price_1080p?: number | null
  video_model_prices?: VideoModelPrices
  web_search_price_per_call?: number | null
  search_price_per_1k?: number | null
  audio_realtime_price_per_min?: number | null
  audio_tts_price_per_million_chars?: number | null
  audio_stt_price_per_hour?: number | null
  peak_rate_enabled?: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
  // 分组利润控制（五个 token 平台；margin/buffer 为小数）
  profit_control_enabled?: boolean
  profit_min_margin?: number
  profit_safety_buffer?: number
  claude_code_only?: boolean
  fallback_group_id?: number | null
  fallback_group_id_on_invalid_request?: number | null
  mcp_xml_inject?: boolean
  supported_model_scopes?: string[]
  model_allowlist?: ModelAllowlist
  codex_models_manifest_config?: CodexModelsManifestConfig
  allow_messages_dispatch?: boolean
  allow_live?: boolean
  default_mapped_model?: string
  messages_dispatch_model_config?: OpenAIMessagesDispatchModelConfig
  model_routing?: Record<string, number[]> | null
  model_routing_enabled?: boolean
  rpm_limit?: number
  max_reasoning_effort?: string
  max_reasoning_effort_over_limit?: string
  reasoning_effort_mappings?: ReasoningEffortMapping[]
  require_oauth_only?: boolean
  require_privacy_set?: boolean
  copy_accounts_from_group_ids?: number[]
}
