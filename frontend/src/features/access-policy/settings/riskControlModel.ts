import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type RiskControlDraft = Pick<SettingsDraft, 'risk_control_enabled' | 'cyber_session_block_enabled' | 'cyber_session_block_ttl_seconds' | 'cyber_policy_user_allowlist'>
export const riskControlFields = ['risk_control_enabled', 'cyber_session_block_enabled', 'cyber_session_block_ttl_seconds', 'cyber_policy_user_allowlist'] as const
export function projectRiskControl(settings: SettingsDraft): RiskControlDraft {
  return {
    risk_control_enabled: structuredClone(toRaw(settings.risk_control_enabled ?? (false))),
    cyber_session_block_enabled: structuredClone(toRaw(settings.cyber_session_block_enabled ?? (false))),
    cyber_session_block_ttl_seconds: structuredClone(toRaw(settings.cyber_session_block_ttl_seconds ?? (3600))),
    cyber_policy_user_allowlist: structuredClone(toRaw(settings.cyber_policy_user_allowlist ?? (""))),
  }
}
