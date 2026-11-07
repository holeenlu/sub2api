import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { CanceledError } from 'axios'
import { useAuthStore } from './auth'
import adminComplianceAPI, { type AdminComplianceStatus } from '../api/compliance'
import { getLocale } from '@/i18n'
import { isChineseLocale } from '@/i18n/localeUtils'
import { BRAND_NAME } from '@/shared/config/project'

// Server acknowledgement phrases remain authoritative; these are neutral unavailable-status defaults.
const FALLBACK_ZH_PHRASE = `我已阅读、理解并同意 ${BRAND_NAME} 部署与运营合规承诺`
const FALLBACK_EN_PHRASE = `I have read, understood, and agree to the ${BRAND_NAME} Deployment and Operation Compliance Commitment`
const JA_DISPLAY_PHRASE = `${BRAND_NAME} のデプロイおよび運用コンプライアンス誓約を読み、理解し、同意しました`
// 后端只有 ack_phrase_zh / ack_phrase_en 两个字段，且 ack_phrase_zh 恒为简体
// （backend/internal/service/admin_compliance.go 的 AdminComplianceAckPhraseZH）。
// 繁体界面要让使用者逐字输入繁体短语，所以这里补一个前端常量；提交时仍按后端
// expectedAdminCompliancePhrase 的预期送简体短语与 language: 'zh'。
// 由 tools/zh-tw/convert.mjs 的 toTraditional() 转换 FALLBACK_ZH_PHRASE 得到。
const ZH_TW_DISPLAY_PHRASE = `我已閱讀、理解並同意 ${BRAND_NAME} 部署與營運合規承諾`

export const useAdminComplianceStore = defineStore('adminCompliance', () => {
  const auth = useAuthStore()
  const status = ref<AdminComplianceStatus | null>(null)
  const loading = ref(false)
  const submitting = ref(false)
  const initialized = ref(false)
  const forceVisible = ref(false)
  let stateVersion = 0
  let fetchSequence = 0
  let statusRequest: AbortController | undefined

  const required = computed(() => status.value?.required === true)
  const shouldShow = computed(() => required.value || forceVisible.value)
  const currentLocale = computed(() => getLocale())
  const isChinese = computed(() => isChineseLocale(currentLocale.value))

  /** 后端比对用的短语（简体 / 英文），也是提交时实际发送的内容。 */
  const submittedPhrase = computed(() => {
    if (isChinese.value) {
      return status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE
    }
    return status.value?.ack_phrase_en || FALLBACK_EN_PHRASE
  })

  /** 后端只认 zh / en 两种语言标记。 */
  const submittedLanguage = computed(() => (isChinese.value ? 'zh' : 'en'))

  /** 界面展示、要求使用者逐字输入的短语；zh-TW 显示繁体。 */
  const expectedPhrase = computed(() => {
    if (currentLocale.value === 'zh-TW') {
      return ZH_TW_DISPLAY_PHRASE
    }
    if (currentLocale.value === 'ja') {
      return JA_DISPLAY_PHRASE
    }
    return submittedPhrase.value
  })

  async function fetchStatus(): Promise<AdminComplianceStatus> {
    const version = stateVersion
    const request = ++fetchSequence
    statusRequest?.abort()
    statusRequest = new AbortController()
    loading.value = true
    try {
      const nextStatus = await adminComplianceAPI.getStatus({ signal: statusRequest.signal })
      if (version !== stateVersion || request !== fetchSequence) return nextStatus
      status.value = nextStatus
      initialized.value = true
      forceVisible.value = nextStatus.required
      return nextStatus
    } catch (error) {
      if (version !== stateVersion || request !== fetchSequence) throw new CanceledError('Compliance scope changed')
      throw error
    } finally {
      if (version === stateVersion && request === fetchSequence) loading.value = false
    }
  }

  async function accept(phrase: string): Promise<AdminComplianceStatus> {
    const version = stateVersion
    const commitmentVersion = status.value?.version
    fetchSequence++
    statusRequest?.abort()
    loading.value = false
    submitting.value = true
    try {
      // 使用者输入的是界面展示的短语（zh-TW 为繁体），后端只接受简体／英文原文，
      // 因此校验通过后改送后端预期的短语。
      const typed = (phrase || '').trim()
      const nextStatus = await adminComplianceAPI.accept({
        phrase: typed === expectedPhrase.value ? submittedPhrase.value : typed,
        language: submittedLanguage.value
      })
      if (version !== stateVersion || commitmentVersion !== status.value?.version) return nextStatus
      status.value = nextStatus
      forceVisible.value = nextStatus.required
      return nextStatus
    } finally {
      if (version === stateVersion) submitting.value = false
    }
  }

  function requireAcknowledgement(partialStatus?: Partial<AdminComplianceStatus>): void {
    fetchSequence++
    statusRequest?.abort()
    loading.value = false
    status.value = {
      required: true,
      version: partialStatus?.version || status.value?.version || 'v2026.06.10',
      document_path_zh: partialStatus?.document_path_zh || status.value?.document_path_zh || 'docs/legal/admin-compliance.zh.md',
      document_path_en: partialStatus?.document_path_en || status.value?.document_path_en || 'docs/legal/admin-compliance.en.md',
      document_url_zh: partialStatus?.document_url_zh ?? status.value?.document_url_zh ?? '',
      document_url_en: partialStatus?.document_url_en ?? status.value?.document_url_en ?? '',
      ack_phrase_zh: partialStatus?.ack_phrase_zh || status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE,
      ack_phrase_en: partialStatus?.ack_phrase_en || status.value?.ack_phrase_en || FALLBACK_EN_PHRASE,
      acknowledgement: status.value?.acknowledgement
    }
    initialized.value = true
    forceVisible.value = true
  }

  function reset(): void {
    stateVersion++
    fetchSequence++
    statusRequest?.abort()
    status.value = null
    loading.value = false
    submitting.value = false
    initialized.value = false
    forceVisible.value = false
  }

  watch(() => auth.identityScopeKey, reset, { flush: 'sync' })
  onScopeDispose(reset)

  return {
    status,
    loading,
    submitting,
    initialized,
    required,
    shouldShow,
    expectedPhrase,
    submittedPhrase,
    submittedLanguage,
    fetchStatus,
    accept,
    requireAcknowledgement,
    reset
  }
})
