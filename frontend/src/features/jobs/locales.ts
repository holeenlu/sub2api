import type { LocaleContributionLoaders } from '@/i18n/messageAssembly'

export const localeLoaders: LocaleContributionLoaders = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh'),
  'zh-TW': () => import('./locales/zh-TW'),
  ja: () => import('./locales/ja')
}
