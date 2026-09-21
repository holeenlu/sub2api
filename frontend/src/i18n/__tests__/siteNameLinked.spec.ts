import { describe, it, expect } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import zhTW from '../locales/zh-TW'
import en from '../locales/en'
import ja from '../locales/ja'
import { BRAND_NAME } from '@/config/brand'

/**
 * 站點名稱由後台設定，文案透過 @:common.siteName 連結取用。
 * stores/app.ts 在載入設定後會用 mergeLocaleMessage 覆蓋 common.siteName，
 * 所以這裡驗證：覆蓋後所有引用處都要跟著變，不能殘留寫死的品牌名。
 *
 * 語言包裡的預設值是 BRAND_NAME（白標 fallback，與後端 service.DefaultSiteName 一致），
 * 這樣設定尚未載入時畫面也不會露出錯誤的品牌。
 */
describe('siteName linked message', () => {
  it.each(['Acme @ Home', 'Acme {AI}', 'Acme | AI', "Acme {'@'} AI"])(
    'preserves runtime site name %j in all lazily loaded locales', async (name) => {
      const { i18n, loadLocaleMessages, setSiteName } = await import('@/i18n')
      const previousLocale = i18n.global.locale.value
      try {
        setSiteName(name)
        for (const locale of ['en', 'zh', 'zh-TW', 'ja'] as const) {
          await loadLocaleMessages(locale)
          i18n.global.locale.value = locale
          expect(i18n.global.t('common.siteName')).toBe(name)
          expect(i18n.global.t('onboarding.admin.welcome.title')).toContain(name)
        }
      } finally {
        setSiteName(BRAND_NAME)
        i18n.global.locale.value = previousLocale
      }
    }
  )
  const makeI18n = () =>
    createI18n({
      legacy: false,
      locale: 'zh',
      fallbackLocale: 'en',
      messages: { zh, 'zh-TW': zhTW, ja, en }
    })

  it('預設值是品牌名', () => {
    const i18n = makeI18n()
    expect(i18n.global.t('common.siteName')).toBe(BRAND_NAME)
  })

  it.each(['zh', 'zh-TW', 'ja', 'en'])('%s：覆蓋後引導教學標題跟著變', (locale) => {
    const i18n = makeI18n()
    i18n.global.locale.value = locale as 'zh' | 'zh-TW' | 'ja' | 'en'
    i18n.global.mergeLocaleMessage(locale, { common: { siteName: 'P2PAPI' } })

    const title = i18n.global.t('onboarding.admin.welcome.title')
    expect(title).toContain('P2PAPI')
    expect(title).not.toContain(BRAND_NAME)
  })

  it.each(['zh', 'zh-TW', 'ja', 'en'])('%s：覆蓋後說明文字也跟著變（含緊接標點的寫法）', (locale) => {
    const i18n = makeI18n()
    i18n.global.locale.value = locale as 'zh' | 'zh-TW' | 'ja' | 'en'
    i18n.global.mergeLocaleMessage(locale, { common: { siteName: 'P2PAPI' } })

    for (const key of [
      'onboarding.admin.welcome.description',
      'onboarding.user.welcome.description',
      'onboarding.admin.groupManage.description',
      'admin.plugins.currentVersion',
      'admin.accounts.usageWindowsHint',
      'admin.settings.openaiFastPolicy.userIdsHint',
      'keys.useKeyModal.grok.codexConfigTomlHint'
    ]) {
      const msg = i18n.global.t(key)
      expect(msg, key).toContain('P2PAPI')
      expect(msg, key).not.toContain(BRAND_NAME)
      expect(msg, key).not.toContain('@:')
    }
  })

  it('設定欄位的 placeholder 保持字面值，因為它顯示的是預設值', () => {
    const i18n = makeI18n()
    i18n.global.mergeLocaleMessage('zh', { common: { siteName: 'P2PAPI' } })
    expect(i18n.global.t('admin.settings.site.siteNamePlaceholder')).toBe(BRAND_NAME)
  })
})
