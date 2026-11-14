// 與 i18n 實例無關的純函式，元件可直接引用而不會觸發 createI18n
// （部分測試完整 mock 了 vue-i18n）。

export type LocaleCode = 'en' | 'zh' | 'zh-TW' | 'ja'

export const DEFAULT_LOCALE: LocaleCode = 'en'

export function isLocaleCode(value: string): value is LocaleCode {
  return value === 'en' || value === 'zh' || value === 'zh-TW' || value === 'ja'
}

/** 依瀏覽器語言挑選預設語言：繁體地區（TW/HK/MO/Hant）用 zh-TW，其餘中文用 zh */
export function detectLocaleFromLanguage(language: string): LocaleCode {
  const lang = (language || '').toLowerCase()
  if (lang === 'ja' || lang.startsWith('ja-')) {
    return 'ja'
  }
  if (!lang.startsWith('zh')) {
    return DEFAULT_LOCALE
  }
  if (/^zh(-hant|-tw|-hk|-mo)/.test(lang) || lang.includes('-hant')) {
    return 'zh-TW'
  }
  return 'zh'
}

/** 是否為中文（簡體或繁體）。中文專屬文件／格式分支請用這個，不要直接比對 'zh'。 */
export function isChineseLocale(locale: string): boolean {
  return (locale || '').toLowerCase().startsWith('zh')
}

/** 是否使用中日文全形標點。 */
export function usesCjkPunctuation(locale: string): boolean {
  const normalized = (locale || '').toLowerCase()
  return normalized.startsWith('zh') || normalized === 'ja' || normalized.startsWith('ja-')
}

/** 給 Intl／toLocaleString 用的 BCP 47 語言標籤 */
export function getIntlLocale(locale: string): string {
  if (locale === 'zh') return 'zh-CN'
  if (locale === 'zh-TW') return 'zh-TW'
  if (locale === 'ja') return 'ja-JP'
  return 'en-US'
}
