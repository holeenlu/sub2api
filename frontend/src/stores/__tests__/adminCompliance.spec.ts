import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import type { AdminComplianceStatus } from '@/api/admin/compliance'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { BRAND_COMPLIANCE_DOCUMENT_URL, BRAND_NAME } from '@/config/brand'

// 后端下发的短语：ack_phrase_zh 恒为简体（backend/internal/service/admin_compliance.go）。
const BACKEND_ZH_PHRASE = `我已阅读、理解并同意 ${BRAND_NAME} 部署与运营合规承诺`
const BACKEND_EN_PHRASE = `I have read, understood, and agree to the ${BRAND_NAME} Deployment and Operation Compliance Commitment`
// zh-TW 界面展示用的繁体短语，由前端常量提供。
const ZH_TW_PHRASE = `我已閱讀、理解並同意 ${BRAND_NAME} 部署與營運合規承諾`
const JA_PHRASE = `${BRAND_NAME} のデプロイおよび運用コンプライアンス誓約を読み、理解し、同意しました`

let currentLocale = 'en'

vi.mock('@/i18n', () => ({
  getLocale: () => currentLocale,
}))

const mockGetStatus = vi.fn()
const mockAccept = vi.fn()

vi.mock('@/api/admin/compliance', () => ({
  default: {
    getStatus: (...args: any[]) => mockGetStatus(...args),
    accept: (...args: any[]) => mockAccept(...args),
  },
}))

function makeStatus(overrides: Partial<AdminComplianceStatus> = {}): AdminComplianceStatus {
  return {
    required: true,
    version: 'v2026.06.10',
    document_path_zh: 'docs/legal/admin-compliance.zh.md',
    document_path_en: 'docs/legal/admin-compliance.en.md',
    document_url_zh: BRAND_COMPLIANCE_DOCUMENT_URL.zh,
    document_url_en: BRAND_COMPLIANCE_DOCUMENT_URL.en,
    ack_phrase_zh: BACKEND_ZH_PHRASE,
    ack_phrase_en: BACKEND_EN_PHRASE,
    ...overrides,
  }
}

describe('useAdminComplianceStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    currentLocale = 'en'
  })

  describe('expectedPhrase', () => {
    it('uses the simplified backend phrase for zh', () => {
      currentLocale = 'zh'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      expect(store.expectedPhrase).toBe(BACKEND_ZH_PHRASE)
      expect(store.submittedPhrase).toBe(BACKEND_ZH_PHRASE)
      expect(store.submittedLanguage).toBe('zh')
    })

    it('shows the traditional frontend phrase for zh-TW but still submits the simplified one', () => {
      currentLocale = 'zh-TW'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      expect(store.expectedPhrase).toBe(ZH_TW_PHRASE)
      expect(store.submittedPhrase).toBe(BACKEND_ZH_PHRASE)
      expect(store.submittedLanguage).toBe('zh')
    })

    it('uses the english backend phrase for en', () => {
      currentLocale = 'en'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      expect(store.expectedPhrase).toBe(BACKEND_EN_PHRASE)
      expect(store.submittedLanguage).toBe('en')
    })

    it('shows a Japanese phrase but submits the backend English phrase', () => {
      currentLocale = 'ja'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      expect(store.expectedPhrase).toBe(JA_PHRASE)
      expect(store.submittedPhrase).toBe(BACKEND_EN_PHRASE)
      expect(store.submittedLanguage).toBe('en')
    })

    it('falls back to bundled phrases when the status has not been fetched', () => {
      // getLocale() 在测试里不是响应式的，computed 会缓存，所以每种语言用新的 pinia。
      const cases: Array<[string, string]> = [
        ['zh', BACKEND_ZH_PHRASE],
        ['zh-TW', ZH_TW_PHRASE],
        ['ja', JA_PHRASE],
        ['en', BACKEND_EN_PHRASE],
      ]
      for (const [locale, phrase] of cases) {
        setActivePinia(createPinia())
        currentLocale = locale
        const store = useAdminComplianceStore()
        expect(store.status).toBeNull()
        expect(store.expectedPhrase).toBe(phrase)
      }
    })
  })

  describe('accept', () => {
    it('sends the simplified phrase and language zh for zh', async () => {
      currentLocale = 'zh'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      mockAccept.mockResolvedValue(makeStatus({ required: false }))

      await store.accept(BACKEND_ZH_PHRASE)

      expect(mockAccept).toHaveBeenCalledWith({
        phrase: BACKEND_ZH_PHRASE,
        language: 'zh',
      })
      expect(store.required).toBe(false)
      expect(store.shouldShow).toBe(false)
    })

    it('translates the typed traditional phrase back to the simplified one for zh-TW', async () => {
      currentLocale = 'zh-TW'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      mockAccept.mockResolvedValue(makeStatus({ required: false }))

      await store.accept(`  ${ZH_TW_PHRASE}  `)

      expect(mockAccept).toHaveBeenCalledWith({
        phrase: BACKEND_ZH_PHRASE,
        language: 'zh',
      })
    })

    it('sends the english phrase and language en for en', async () => {
      currentLocale = 'en'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      mockAccept.mockResolvedValue(makeStatus({ required: false }))

      await store.accept(BACKEND_EN_PHRASE)

      expect(mockAccept).toHaveBeenCalledWith({
        phrase: BACKEND_EN_PHRASE,
        language: 'en',
      })
    })

    it('translates the typed Japanese phrase back to the English backend phrase', async () => {
      currentLocale = 'ja'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      mockAccept.mockResolvedValue(makeStatus({ required: false }))

      await store.accept(JA_PHRASE)

      expect(mockAccept).toHaveBeenCalledWith({
        phrase: BACKEND_EN_PHRASE,
        language: 'en',
      })
    })

    it('passes a mismatched input through unchanged so the backend rejects it', async () => {
      currentLocale = 'zh-TW'
      const store = useAdminComplianceStore()
      store.status = makeStatus()
      mockAccept.mockRejectedValue(new Error('confirmation phrase does not match'))

      await expect(store.accept('我不同意')).rejects.toThrow()
      expect(mockAccept).toHaveBeenCalledWith({
        phrase: '我不同意',
        language: 'zh',
      })
      expect(store.submitting).toBe(false)
    })
  })

  describe('requireAcknowledgement', () => {
    it('builds a blocking status with the bundled defaults', () => {
      currentLocale = 'zh-TW'
      const store = useAdminComplianceStore()
      store.requireAcknowledgement()
      expect(store.shouldShow).toBe(true)
      expect(store.status?.ack_phrase_zh).toBe(BACKEND_ZH_PHRASE)
      expect(store.expectedPhrase).toBe(ZH_TW_PHRASE)
    })
  })
})
