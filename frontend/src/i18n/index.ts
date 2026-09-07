import { createI18n } from 'vue-i18n'
import {
  DEFAULT_LOCALE,
  detectLocaleFromLanguage,
  getIntlLocale,
  isChineseLocale,
  isLocaleCode,
  type LocaleCode
} from './localeUtils'

// zh-TW（繁體中文／台灣用語）語言包由 tools/zh-tw/gen-locale.mjs 依 zh 自動產生，
// 缺漏的 key 依序回退到 zh、en。
export { detectLocaleFromLanguage, getIntlLocale, isChineseLocale, type LocaleCode }

type LocaleMessages = Record<string, any>

const LOCALE_KEY = 'sub2api_locale'

const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh'),
  'zh-TW': () => import('./locales/zh-TW')
}

// 語言回退鏈：zh-TW → zh → en；其餘 → en
const FALLBACK_CHAIN: Record<LocaleCode, LocaleCode[]> = {
  en: [],
  zh: ['en'],
  'zh-TW': ['zh', 'en']
}

function getDefaultLocale(): LocaleCode {
  const saved = localStorage.getItem(LOCALE_KEY)
  if (saved && isLocaleCode(saved)) {
    return saved
  }

  return detectLocaleFromLanguage(navigator.language)
}

export const i18n = createI18n({
  legacy: false,
  locale: getDefaultLocale(),
  fallbackLocale: { ...FALLBACK_CHAIN, default: [DEFAULT_LOCALE] },
  messages: {},
  // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
  // 这些内容是内部定义的，不存在 XSS 风险
  warnHtmlMessage: false
})

const loadedLocales = new Set<LocaleCode>()

// 後台可自訂站點名稱，文案用 @:common.siteName 連結取用（預設值是語言包裡的品牌名）。
// 這裡保存目前值，因為語言包是延遲載入且 setLocaleMessage 會整包取代，
// 每次載入語言包後都得重新套用一次，否則會被語言包裡的預設值蓋回去。
let currentSiteName = ''

function applySiteName(locale: LocaleCode): void {
  if (!currentSiteName) return
  // A message function preserves user text without compiling @, braces or pipes.
  const name = currentSiteName
  i18n.global.mergeLocaleMessage(locale, { common: { siteName: () => name } })
}

/** 設定站點名稱，並同步到所有已載入的語言包 */
export function setSiteName(name: string): void {
  const trimmed = name.trim()
  if (!trimmed || trimmed === currentSiteName) return
  currentSiteName = trimmed
  loadedLocales.forEach(applySiteName)
}

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) {
    return
  }

  // 回退語言是延遲載入的，必須一起載入，fallbackLocale 才有內容可回退
  for (const fallback of FALLBACK_CHAIN[locale]) {
    await loadLocaleMessages(fallback)
  }

  const loader = localeLoaders[locale]
  const module = await loader()
  i18n.global.setLocaleMessage(locale, module.default)
  loadedLocales.add(locale)
  applySiteName(locale)
}

export async function initI18n(): Promise<void> {
  const current = getLocale()
  await loadLocaleMessages(current)
  document.documentElement.setAttribute('lang', current)
}

export async function setLocale(locale: string): Promise<void> {
  if (!isLocaleCode(locale)) {
    return
  }

  await loadLocaleMessages(locale)
  i18n.global.locale.value = locale
  localStorage.setItem(LOCALE_KEY, locale)
  document.documentElement.setAttribute('lang', locale)

  // 同步更新浏览器页签标题，使其跟随语言切换
  const { resolveRouteDocumentTitle, applyRouteMetaDescription } = await import('@/router/title')
  const { default: router } = await import('@/router')
  const { useAppStore } = await import('@/stores/app')
  const { useAuthStore } = await import('@/stores/auth')
  const { useAdminSettingsStore } = await import('@/stores/adminSettings')
  const route = router.currentRoute.value
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
  applyRouteMetaDescription(route)
}

export function getLocale(): LocaleCode {
  const current = i18n.global.locale.value
  return isLocaleCode(current) ? current : DEFAULT_LOCALE
}

export const availableLocales = [
  { code: 'en', name: 'English' },
  { code: 'zh', name: '简体中文' },
  { code: 'zh-TW', name: '繁體中文' }
] as const

export default i18n
