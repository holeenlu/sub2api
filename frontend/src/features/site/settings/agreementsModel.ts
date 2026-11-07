import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type AgreementsDraft = Pick<SettingsDraft, 'login_agreement_enabled' | 'login_agreement_mode' | 'login_agreement_updated_at' | 'login_agreement_documents'>
export const agreementsFields = ['login_agreement_enabled', 'login_agreement_mode', 'login_agreement_updated_at', 'login_agreement_documents'] as const
export function projectAgreements(settings: SettingsDraft): AgreementsDraft {
  return {
    login_agreement_enabled: structuredClone(toRaw(settings.login_agreement_enabled ?? (false))),
    login_agreement_mode: structuredClone(toRaw(settings.login_agreement_mode ?? ("modal"))),
    login_agreement_updated_at: structuredClone(toRaw(settings.login_agreement_updated_at ?? (""))),
    login_agreement_documents: structuredClone(toRaw(settings.login_agreement_documents ?? ([]))),
  }
}
