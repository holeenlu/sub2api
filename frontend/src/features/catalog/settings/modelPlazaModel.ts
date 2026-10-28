import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ModelPlazaDraft = Pick<SettingsDraft, 'model_plaza_enabled' | 'model_plaza_require_auth' | 'model_plaza_description'>
export const modelPlazaFields = ['model_plaza_enabled', 'model_plaza_require_auth', 'model_plaza_description'] as const
export function projectModelPlaza(settings: SettingsDraft): ModelPlazaDraft {
  return {
    model_plaza_enabled: structuredClone(toRaw(settings.model_plaza_enabled ?? (false))),
    model_plaza_require_auth: structuredClone(toRaw(settings.model_plaza_require_auth ?? (false))),
    model_plaza_description: structuredClone(toRaw(settings.model_plaza_description ?? (''))),
  }
}
