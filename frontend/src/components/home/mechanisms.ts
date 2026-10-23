import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { isChineseLocale } from '@/i18n/localeUtils'

/** 七项机制的 key 名与展示顺序。索引 = 编号 ①-⑦。 */
export const MECHANISM_NAMES = [
  'marketplace',
  'routing',
  'cost',
  'failover',
  'billing',
  'gateway',
  'agents'
] as const

export type MechanismName = (typeof MECHANISM_NAMES)[number]

/** ①②③…⑦，与 MECHANISM_NAMES 同序。 */
export const BADGES = ['①', '②', '③', '④', '⑤', '⑥', '⑦'] as const

export const BADGE_CLASS =
  'inline-flex h-[22px] w-[22px] flex-none items-center justify-center rounded-full bg-gradient-to-br from-primary-500 to-primary-600 text-xs font-bold text-white shadow-sm shadow-primary-500/40'

export const BADGE_PLAIN_CLASS =
  'inline-flex h-[22px] w-[22px] flex-none items-center justify-center rounded-full bg-gray-200 text-xs font-bold text-gray-500 dark:bg-dark-700 dark:text-dark-400'

/**
 * 中文界面在机制标题下加一行英文原名（客户要求同时看到两种写法）；
 * 英文界面只显示英文，所以不重复。英文取自 en locale，不在元件里硬写第二份文字。
 */
export function useMechanismEnglishTitle() {
  const { t, locale } = useI18n()

  const showEnglish = computed(() => isChineseLocale(String(locale.value)))

  function englishTitle(name: MechanismName): string {
    if (!showEnglish.value) return ''
    return t(`home.mechanisms.${name}.title`, {}, { locale: 'en' })
  }

  return { showEnglish, englishTitle }
}
