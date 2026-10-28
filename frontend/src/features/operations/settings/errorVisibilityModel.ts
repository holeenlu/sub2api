import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ErrorVisibilityDraft = Pick<SettingsDraft, 'allow_user_view_error_requests'>
export const errorVisibilityFields = ['allow_user_view_error_requests'] as const
export function projectErrorVisibility(settings: SettingsDraft): ErrorVisibilityDraft {
  return {
    allow_user_view_error_requests: structuredClone(toRaw(settings.allow_user_view_error_requests ?? (false))),
  }
}
