import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, relative, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import baseline from './hardcodedLocaleUsage.baseline.json'

// 棘輪測試（ratchet）：鎖住「繞過 i18n / 直接比對語系」的既有寫法數量，只准減少不准增加。
//
//   localText(zh, en)          → 應改成 i18n key（否則 zh-TW 會拿到簡體）
//   labelZh / labelEn          → 同上，資料表裡寫死雙語
//   startsWith('zh') / === 'zh' / 'zh-CN'
//                              → 應改用 @/i18n/localeUtils 的 isChineseLocale / getIntlLocale
//   toLocaleString('zh-CN', …) → 應改用 getIntlLocale(locale.value)
//
// 新增任何一種寫法會讓這個測試失敗；移除之後請把 baseline 調低，讓數量只會單向下降。

const HERE = dirname(fileURLToPath(import.meta.url))
const SRC = resolve(HERE, '../..')
const REPO_ROOT = resolve(SRC, '../..')

const PATTERNS = {
  // localText('中文', 'English') 之類的行內雙語函式
  localText: /\blocalText\s*\(/g,
  // { labelZh: '…', labelEn: '…' } 形式的行內雙語資料
  labelZhEn: /\blabel(?:Zh|En)\s*:/g,
  // 行內語系比較：startsWith('zh') / === 'zh' / 'zh-CN'
  inlineLocaleComparison: /startsWith\(\s*(['"])zh\1\s*\)|===\s*(['"])zh\2|(['"])zh\3\s*===|(['"])zh-CN\4/g,
  // Intl 格式化寫死簡體中文
  toLocaleZhCN: /toLocale(?:String|DateString|TimeString)\(\s*(['"])zh-CN\1/g,
} as const

type PatternName = keyof typeof PATTERNS

function isExcluded(relPath: string): boolean {
  return (
    relPath.includes('__tests__/') ||
    relPath.endsWith('.spec.ts') ||
    relPath.endsWith('.test.ts') ||
    relPath.startsWith('i18n/locales/') ||
    relPath === 'i18n/localeUtils.ts'
  )
}

// 手寫遞迴，不用 fs.globSync（Node 20 沒有）
function listSourceFiles(dir: string, prefix = '', out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const rel = prefix ? `${prefix}/${entry.name}` : entry.name
    if (entry.isDirectory()) {
      listSourceFiles(resolve(dir, entry.name), rel, out)
    } else if (/\.(vue|ts)$/.test(entry.name) && !isExcluded(rel)) {
      out.push(rel)
    }
  }
  return out
}

function scan(): Record<PatternName, { count: number; hits: string[] }> {
  const result = {} as Record<PatternName, { count: number; hits: string[] }>
  for (const name of Object.keys(PATTERNS) as PatternName[]) {
    result[name] = { count: 0, hits: [] }
  }

  for (const file of listSourceFiles(SRC)) {
    const lines = readFileSync(resolve(SRC, file), 'utf8').split('\n')
    lines.forEach((line, index) => {
      for (const name of Object.keys(PATTERNS) as PatternName[]) {
        const matches = line.match(PATTERNS[name])
        if (!matches) continue
        result[name].count += matches.length
        result[name].hits.push(`src/${file}:${index + 1}`)
      }
    })
  }
  return result
}

const BASELINE_PATH = relative(REPO_ROOT, resolve(HERE, 'hardcodedLocaleUsage.baseline.json'))

describe('hardcoded locale usage ratchet', () => {
  const scanned = scan()

  it('covers every baseline pattern', () => {
    expect(Object.keys(baseline).sort()).toEqual(Object.keys(PATTERNS).sort())
  })

  for (const name of Object.keys(PATTERNS) as PatternName[]) {
    it(`does not add new "${name}" sites`, () => {
      const allowed = (baseline as Record<string, number>)[name]
      const { count, hits } = scanned[name]
      const message =
        `frontend/src 裡 "${name}" 的數量從 ${allowed} 變成 ${count}。\n` +
        '請改用 i18n key，或用 @/i18n/localeUtils 的 isChineseLocale / getIntlLocale 取代行內語系比較。\n' +
        `若你是在移除這類寫法，請把 ${BASELINE_PATH} 的 "${name}" 調降到 ${count}。\n` +
        `目前命中位置：\n  ${hits.join('\n  ')}`
      expect(count, message).toBeLessThanOrEqual(allowed)
    })
  }
})
