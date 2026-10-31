import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type FastPolicyDraft = Pick<SettingsDraft, 'openai_fast_policy_settings'>
export const fastPolicyFields = ['openai_fast_policy_settings'] as const
export function projectFastPolicy(settings: SettingsDraft): FastPolicyDraft {
  return {
    openai_fast_policy_settings: structuredClone(toRaw(settings.openai_fast_policy_settings)),
  }
}
