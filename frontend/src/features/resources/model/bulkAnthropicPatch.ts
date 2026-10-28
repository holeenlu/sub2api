import type { BulkEditPermissions, BulkTargetEligibility } from './bulkTargetEligibility'

export interface BulkAnthropicDraft {
  readonly rpm?: {
    readonly enabled: boolean
    readonly baseRpm: number | null
    readonly strategy: 'tiered' | 'sticky_exempt'
    readonly stickyBuffer: number | null
  }
  readonly userMsgQueueMode?: string | null
}

export type BulkAnthropicPatch = {
  base_rpm?: number
  rpm_strategy?: 'tiered' | 'sticky_exempt' | ''
  rpm_sticky_buffer?: number
  user_msg_queue_mode?: string
  user_msg_queue_enabled?: boolean
}

export function projectBulkAnthropic(
  draft: BulkAnthropicDraft,
  permissions: BulkEditPermissions,
  target: BulkTargetEligibility
): BulkAnthropicPatch {
  const extra: BulkAnthropicPatch = {}
  if (!permissions.allowTransportEdit || !target.anthropicOAuth) return extra
  const rpm = draft.rpm
  if (rpm) {
    if (rpm.enabled && rpm.baseRpm != null && rpm.baseRpm > 0) {
      extra.base_rpm = rpm.baseRpm
      extra.rpm_strategy = rpm.strategy
      if (rpm.stickyBuffer != null && rpm.stickyBuffer > 0) extra.rpm_sticky_buffer = rpm.stickyBuffer
    } else {
      // JSONB merge cannot delete keys: explicit reset values restore reader defaults.
      extra.base_rpm = 0
      extra.rpm_strategy = ''
      extra.rpm_sticky_buffer = 0
    }
  }
  // UMQ selection is independent of RPM; empty string removes the account override.
  if (draft.userMsgQueueMode != null) {
    extra.user_msg_queue_mode = draft.userMsgQueueMode
    extra.user_msg_queue_enabled = false
  }
  return extra
}
