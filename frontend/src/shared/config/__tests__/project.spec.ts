import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { BRAND_NAME, PROJECT_ID, CLIENT_PROVIDER_ID, CLIENT_API_KEY_ENV, CLIENT_BASE_URL_ENV, BATCH_IMAGE_SKILL_NAME, brandFilePrefix } from '../project'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const assembledJaMessages = await loadApplicationLocale('ja')
const assembledZhTWMessages = await loadApplicationLocale('zh-TW')
const enDashboard = assembledEnMessages
const zhDashboard = assembledZhMessages
const jaDashboard = assembledJaMessages
const enOverview = assembledEnMessages.admin
const zhOverview = assembledZhMessages.admin
const jaOverview = assembledJaMessages.admin
const enCommon = assembledEnMessages
const enDocs = assembledEnMessages
const zhCommon = assembledZhMessages
const zhDocs = assembledZhMessages
const zhTwCommon = assembledZhTWMessages
const zhTwDocs = assembledZhTWMessages
const jaCommon = assembledJaMessages
const jaDocs = assembledJaMessages














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


describe('stable technical client identifiers', () => {
  it('keeps protocol identifiers independent of a configured display/file name', () => {
    expect(brandFilePrefix('KDAN Operator "Site"')).toBe('kdan-operator-site')
    expect(PROJECT_ID).toBe('sub2api')
    expect(CLIENT_PROVIDER_ID).toBe('sub2api')
    expect(CLIENT_API_KEY_ENV).toBe('SUB2API_API_KEY')
    expect(CLIENT_BASE_URL_ENV).toBe('SUB2API_BASE_URL')
    expect(BATCH_IMAGE_SKILL_NAME).toBe('sub2api-batch-image')
  })

  it.each([
    ['en', enDashboard, enOverview], ['zh', zhDashboard, zhOverview], ['ja', jaDashboard, jaOverview]
  ])('uses neutral generated-configuration hints and new bucket examples in %s', (_locale, dashboard, overview) => {
    const hints = JSON.stringify(dashboard)
    expect(hints).toContain(CLIENT_API_KEY_ENV)
    expect(hints).toContain(CLIENT_BASE_URL_ENV)
    expect(hints).not.toMatch(/KDAN_API_KEY|TAPMODELS_API_KEY/)
    expect(JSON.stringify(overview)).toContain(PROJECT_ID + '-backups')
    expect(JSON.stringify(overview)).not.toMatch(/kdan-backups|tapmodels-backups/)
  })
})
