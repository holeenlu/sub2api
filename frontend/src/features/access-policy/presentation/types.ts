import type { GroupPlatform } from '@/features/access-policy/api/groupTypes';
import type { SubscriptionType } from '@/features/subscriptions/api/subscriptionPolicyTypes';

export interface GroupBadgeProps {
  readonly name: string
  readonly platform?: GroupPlatform
  readonly subscriptionType?: SubscriptionType
  readonly rateMultiplier?: number
  readonly userRateMultiplier?: number | null // 用户专属倍率
  readonly peakRateEnabled?: boolean
  readonly peakStart?: string
  readonly peakEnd?: string
  readonly peakRateMultiplier?: number
  readonly showRate?: boolean
  /** Allow full group names to wrap in layouts that have room for multiple lines. */
  readonly truncate?: boolean
  readonly daysRemaining?: number | null // 剩余天数（订阅类型时使用）
  /**
   * 订阅分组默认在右侧 label 展示"订阅"或剩余天数；
   * 开启后订阅分组也改为显示倍率（保留订阅主题色 label，配合可用渠道这类
   * 只关心费率、不关心有效期的场景）。
   */
  readonly alwaysShowRate?: boolean
}

export interface GroupOptionItemProps {
  readonly name: string
  readonly platform: GroupPlatform
  readonly subscriptionType?: SubscriptionType
  readonly rateMultiplier?: number
  readonly userRateMultiplier?: number | null
  readonly peakRateEnabled?: boolean
  readonly peakStart?: string
  readonly peakEnd?: string
  readonly peakRateMultiplier?: number
  readonly description?: string | null
  readonly selected?: boolean
  readonly showCheckmark?: boolean
}

export interface GroupSelectorProps {
  readonly modelValue: readonly number[]
  // Safe supplier projections omit billing and consumption-policy fields.
  readonly groups: readonly GroupSelectionOption[]
  readonly platform?: GroupPlatform // Optional platform filter
  readonly mixedScheduling?: boolean // For antigravity accounts: allow anthropic/gemini groups
  readonly searchable?: boolean | 'auto'
}

export interface GroupCapacityBadgeProps {
  readonly concurrencyUsed?: number
  readonly concurrencyMax?: number
  readonly sessionsUsed?: number
  readonly sessionsMax?: number
  readonly rpmUsed?: number
  readonly rpmMax?: number
}

/** Read-only display projection; supplier callers may omit billing metadata. */
export interface GroupSelectionOption {
  readonly id: number
  readonly name: string
  readonly description?: string | null
  readonly platform?: GroupPlatform
  readonly subscription_type?: SubscriptionType | null
  readonly rate_multiplier?: number | null
  readonly account_count?: number
}
