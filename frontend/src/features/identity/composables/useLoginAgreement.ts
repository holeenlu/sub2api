import { computed, ref } from 'vue'
import type { LoginAgreementDocument } from '@/features/site/api/publicSettingsTypes';

const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

export function useLoginAgreement(onRejected: () => void) {
  const loginAgreementEnabled = ref<boolean>(false)
  const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
  const loginAgreementUpdatedAt = ref<string>('')
  const loginAgreementRevision = ref<string>('')
  const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
  const agreementAccepted = ref<boolean>(false)
  const showAgreementModal = ref<boolean>(false)

  const agreementGateActive = computed(() => loginAgreementEnabled.value && !agreementAccepted.value)

  function applyLoginAgreementSettings(settings: {
    login_agreement_enabled?: boolean
    login_agreement_mode?: string
    login_agreement_updated_at?: string
    login_agreement_revision?: string
    login_agreement_documents?: LoginAgreementDocument[]
  }): void {
    const documents = Array.isArray(settings.login_agreement_documents)
      ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
      : []
    loginAgreementDocuments.value = documents
    loginAgreementEnabled.value = settings.login_agreement_enabled === true && documents.length > 0
    loginAgreementMode.value = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
    loginAgreementUpdatedAt.value = settings.login_agreement_updated_at || ''
    loginAgreementRevision.value =
      settings.login_agreement_revision ||
      `${loginAgreementUpdatedAt.value}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

    agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
    showAgreementModal.value =
      loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
  }

  function hasAcceptedLoginAgreement(revision: string): boolean {
    if (!revision) {
      return false
    }
    try {
      const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
      if (!raw) {
        return false
      }
      const parsed = JSON.parse(raw) as { revision?: string }
      return parsed.revision === revision
    } catch {
      return false
    }
  }

  function acceptLoginAgreement(): void {
    if (loginAgreementRevision.value) {
      localStorage.setItem(
        LOGIN_AGREEMENT_STORAGE_KEY,
        JSON.stringify({
          revision: loginAgreementRevision.value,
          accepted_at: new Date().toISOString()
        })
      )
    }
    agreementAccepted.value = true
    showAgreementModal.value = false
  }

  function rejectLoginAgreement(): void {
    localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
    agreementAccepted.value = false
    showAgreementModal.value = false
    onRejected()
  }

  return { loginAgreementEnabled, loginAgreementMode, loginAgreementUpdatedAt, loginAgreementDocuments, agreementAccepted, showAgreementModal, agreementGateActive, applyLoginAgreementSettings, acceptLoginAgreement, rejectLoginAgreement }
}
