import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { BRAND_NAME } from '../project'
import enCommon from '@/i18n/locales/en/common'
import enDocs from '@/i18n/locales/en/docs'
import zhCommon from '@/i18n/locales/zh/common'
import zhDocs from '@/i18n/locales/zh/docs'
import zhTwCommon from '@/i18n/locales/zh-TW/common'
import zhTwDocs from '@/i18n/locales/zh-TW/docs'
import jaCommon from '@/i18n/locales/ja/common'
import jaDocs from '@/i18n/locales/ja/docs'

const locales = [
  { locale: 'en', common: enCommon, docs: enDocs },
  { locale: 'zh', common: zhCommon, docs: zhDocs },
  { locale: 'zh-TW', common: zhTwCommon, docs: zhTwDocs },
  { locale: 'ja', common: jaCommon, docs: jaDocs }
]

describe.each(locales)('project identity in $locale', ({ locale, common, docs }) => {
  it('uses the neutral default until explicit site settings arrive', () => {
    const i18n = createI18n({ legacy: false, locale, messages: { [locale]: { ...common, ...docs } } })
    try {
      expect(BRAND_NAME).toBe('sub2api')
      expect(i18n.global.t('common.siteName')).toBe(BRAND_NAME)
      expect(i18n.global.t('docs.pages.overview.title')).toContain(BRAND_NAME)

      i18n.global.mergeLocaleMessage(locale, { common: { siteName: 'Operator Gateway' } })
      expect(i18n.global.t('docs.pages.overview.title')).toContain('Operator Gateway')
      expect(i18n.global.t('docs.pages.codex.description')).toContain('Operator Gateway')
      expect(i18n.global.t('docs.pages.overview.title')).not.toContain('sub2api')
    } finally {
      i18n.dispose()
    }
  })
})
