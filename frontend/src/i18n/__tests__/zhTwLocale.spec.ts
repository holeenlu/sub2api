import { describe, expect, it } from 'vitest'

import zh from '../locales/zh'
import zhTW from '../locales/zh-TW'
import { detectLocaleFromLanguage, getIntlLocale, isChineseLocale } from '../localeUtils'

// zh-TW 由 tools/zh-tw/gen-locale.mjs 依 zh 自動產生。本測試固化兩件事：
// 1. key 集合與 zh 完全一致（有人改了 zh 卻沒重跑產生腳本時直接失敗）
// 2. 內容確實是繁體、且套了台灣用語（不是把 zh 原樣複製過來）
function flattenKeys(node: unknown, prefix = '', out: string[] = []): string[] {
  if (Array.isArray(node)) {
    node.forEach((item, index) => flattenKeys(item, `${prefix}[${index}]`, out))
  } else if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
      flattenKeys(value, prefix ? `${prefix}.${key}` : key, out)
    }
  } else {
    out.push(prefix)
  }
  return out
}

function collectStrings(node: unknown, out: string[] = []): string[] {
  if (typeof node === 'string') out.push(node)
  else if (Array.isArray(node)) node.forEach((item) => collectStrings(item, out))
  else if (node && typeof node === 'object') Object.values(node).forEach((v) => collectStrings(v, out))
  return out
}

// 只在簡體才會出現的常見字；zh-TW 語言包裡不該有任何一個
const SIMPLIFIED_ONLY = /[这为发时说见开关电线经东车让门问间业务号请设录网页数据应该们从会来实现动没对头点击复删确认账]/

describe('zh-TW locale', () => {
  it('has exactly the same keys as zh', () => {
    expect(flattenKeys(zhTW).sort()).toEqual(flattenKeys(zh).sort())
  })

  it('contains no simplified-only characters', () => {
    const offenders = collectStrings(zhTW).filter((s) => SIMPLIFIED_ONLY.test(s))
    expect(offenders).toEqual([])
  })

  it('uses Taiwan vocabulary rather than mainland terms', () => {
    const text = collectStrings(zhTW).join('\n')
    for (const mainland of ['郵箱', '賬號', '令牌', '當前', '自定義', '默認', '軟件', '服務器', '數據庫', '用戶(?!端)']) {
      expect(text, `should not contain ${mainland}`).not.toMatch(new RegExp(mainland))
    }
    expect(zhTW.common.save).toBe('儲存')
    expect(zhTW.common.loading).toBe('載入中...')
  })

  it('detects browser language', () => {
    expect(detectLocaleFromLanguage('zh-TW')).toBe('zh-TW')
    expect(detectLocaleFromLanguage('zh-Hant-TW')).toBe('zh-TW')
    expect(detectLocaleFromLanguage('zh-HK')).toBe('zh-TW')
    expect(detectLocaleFromLanguage('zh-CN')).toBe('zh')
    expect(detectLocaleFromLanguage('zh')).toBe('zh')
    expect(detectLocaleFromLanguage('en-US')).toBe('en')
  })

  it('maps locale helpers', () => {
    expect(isChineseLocale('zh-TW')).toBe(true)
    expect(isChineseLocale('zh')).toBe(true)
    expect(isChineseLocale('en')).toBe(false)
    expect(getIntlLocale('zh-TW')).toBe('zh-TW')
    expect(getIntlLocale('zh')).toBe('zh-CN')
    expect(getIntlLocale('en')).toBe('en-US')
  })
})
