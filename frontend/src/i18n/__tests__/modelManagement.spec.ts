import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '../locales/en/modelManagement'
import zh from '../locales/zh/modelManagement'
import zhTW from '../locales/zh-TW/modelManagement'
import ja from '../locales/ja/modelManagement'

// The model page constructs these paths from tab, dialog, action and price-field
// IDs. Static t('literal') scanning cannot detect a missing translation here.
const keys = [
  'models', 'pricing', 'editPrice', 'basePrices', 'resetConfirm', 'syncConfirm', 'discardConfirm',
  'maxContext', 'maxOutput', 'reasoningLevels', 'modalities',
  'on_adaptive', 'on_enabled', 'passthrough', 'strip',
  'convert_to_enabled', 'convert_to_adaptive',
  'input', 'output', 'cache_read', 'cache_5m', 'cache_1h',
] as const

describe('model management dynamic translations', () => {
  for (const [locale, messages] of Object.entries({ en, zh, 'zh-TW': zhTW, ja })) {
    it(`renders tab, configuration and confirmation labels in ${locale}`, () => {
      const { global } = createI18n({ legacy: false, locale, fallbackLocale: false, messages: { [locale]: messages } })
      for (const suffix of keys) {
        const key = `modelManagement.${suffix}`
        expect(global.te(key), key).toBe(true)
        expect(global.t(key), key).not.toBe(key)
        expect(global.t(key).trim(), key).not.toBe('')
      }
    })
  }
})
