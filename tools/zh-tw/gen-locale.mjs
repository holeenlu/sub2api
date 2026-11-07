#!/usr/bin/env node
// 由簡體語言包產生繁體（台灣）語言包。
//
//   frontend/src/i18n/locales/zh/**/*.ts       →  frontend/src/i18n/locales/zh-TW/**/*.ts
//   frontend/src/features/*/locales/zh/**/*.ts → frontend/src/features/*/locales/zh-TW/**/*.ts
//   frontend/src/features/site/docs/content/zh/**/*.md       →  frontend/src/features/site/docs/content/zh-TW/**/*.md
//   docs/legal/admin-compliance.zh.md          →  docs/legal/admin-compliance.zh-TW.md
//
// 用法（在 repo 任何位置執行皆可）：
//   node tools/zh-tw/gen-locale.mjs            # 產生／覆寫 zh-TW
//   node tools/zh-tw/gen-locale.mjs --check    # 只比對，已提交的 zh-TW 與重新產生的結果不同時離開碼 1
//   node tools/zh-tw/gen-locale.mjs --locales-only # 不寫 docs/legal
//   node tools/zh-tw/gen-locale.mjs --locales-only --prune-generated-orphans # 僅在來源遷移完成後清理已核實產生物
//
// zh-TW 是產生物，請不要手改：
//   - 通用詞彙問題 → 改 convert.mjs 的 CORRECTIONS / TW_VOCAB
//   - 只在特定句子才成立的修正 → 改下方 OVERRIDES
// 改完重跑本腳本即可。轉換只動中文字元、逐行處理，檔案結構與行數與 zh 完全一致。

import { readFileSync, writeFileSync, mkdirSync, globSync, existsSync, lstatSync, unlinkSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { convertSource } from './convert.mjs'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const LOCALE_SOURCES = [
  'frontend/src/i18n/locales/zh/**/*.ts',
  'frontend/src/features/*/locales/zh/**/*.ts',
]
const DOCS_ZH_DIR = join(ROOT, 'frontend/src/features/site/docs/content/zh')
const DOCS_TW_DIR = join(ROOT, 'frontend/src/features/site/docs/content/zh-TW')
const LEGAL_ZH = join(ROOT, 'docs/legal/admin-compliance.zh.md')
const LEGAL_TW = join(ROOT, 'docs/legal/admin-compliance.zh-TW.md')

// 逐句覆寫：字典層級無法安全表達、只對特定句子成立的修正。
// 在轉換結果（繁體）上做字面替換；順序有意義。
const OVERRIDES = [
  [/通過一個 API 使用不同 AI 模型。\\n減少接入與管理的負擔/g, '透過一個 API 使用不同 AI 模型。\\n減少串接與管理的負擔'],
  [/\/docs-assets\/(client-(?:codex|claude))-zh\.png/g, '/docs-assets/$1-zh-TW.png'],
  // 台灣的計量詞：記錄／資料／日誌用「筆」，公告／訊息用「則」；規則、Prompt 等保留「條」
  [/(\{\w+\}|\d+) 條(?=新公告|公告|訊息|通知)/g, '$1 則'],
  [/第一條(?=公告|訊息|通知)/g, '第一則'],
  [/(\{\w+\}|\d+) 條(?=['，。；\s]|$|稽核|操作|日誌|記錄|資料|事件|用量)/g, '$1 筆'],
  [/單條刪除/g, '單筆刪除'],
  // 語言包裡單獨成值的 '應用'（apply / applyMultiplier 等按鈕）是動詞 apply
  [/: '應用'/g, ": '套用'"],
  [/最早的一條/g, '最早的一筆'],
  [/多條請求值/g, '多筆請求值'],
  // 品牌標語：這裡的「通過」是 via（透過），「接入」台灣說「串接」
  [/通過一個 API 使用不同 AI 模型/g, '透過一個 API 使用不同 AI 模型'],
  [/減少接入與管理的負擔/g, '減少串接與管理的負擔'],
]

const HEADER_TS =
  '// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。\n' +
  '// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。\n'

function applyOverrides(text) {
  return OVERRIDES.reduce((s, [from, to]) => s.replace(from, to), text)
}

function generate(src, { header = '' } = {}) {
  return header + applyOverrides(convertSource(src))
}

function localeFiles(language) {
  return LOCALE_SOURCES.flatMap((pattern) =>
    globSync(pattern.replace('/locales/zh/', `/locales/${language}/`), { cwd: ROOT }),
  ).sort()
}

function targets(localesOnly) {
  const out = []
  for (const file of localeFiles('zh')) {
    const from = join(ROOT, file)
    const to = join(ROOT, file.replace('/locales/zh/', '/locales/zh-TW/'))
    out.push({ from, to, header: HEADER_TS })
  }
  if (localesOnly) return out
  for (const file of globSync('**/*.md', { cwd: DOCS_ZH_DIR })) {
    out.push({ from: join(DOCS_ZH_DIR, file), to: join(DOCS_TW_DIR, file), header: '' })
  }
  out.push({ from: LEGAL_ZH, to: LEGAL_TW, header: '' })
  return out
}

function main() {
  const check = process.argv.includes('--check')
  const localesOnly = process.argv.includes('--locales-only')
  const prune = process.argv.includes('--prune-generated-orphans')
  const allowed = new Set(['--check', '--locales-only', '--prune-generated-orphans'])
  if (process.argv.slice(2).some((arg) => !allowed.has(arg)) || (check && prune)) {
    throw new Error('Use --check or --prune-generated-orphans, optionally with --locales-only')
  }
  const entries = targets(localesOnly)
  const expectedLocales = new Set(entries.filter((entry) => entry.header === HEADER_TS).map((entry) => entry.to))
  const orphans = localeFiles('zh-TW').map((file) => join(ROOT, file)).filter((file) => !expectedLocales.has(file))
  let changed = 0
  let unresolvedOrphans = 0
  for (const file of orphans) {
    const regular = lstatSync(file).isFile() && !lstatSync(file).isSymbolicLink()
    const generated = regular && readFileSync(file, 'utf8').startsWith(HEADER_TS)
    if (prune && generated) {
      unlinkSync(file)
      console.log(`[zh-tw] 已清理無來源產生物：${relative(ROOT, file)}`)
    } else {
      unresolvedOrphans++
      console.error(`[zh-tw] 無對應 zh 來源${generated ? '（已確認產生標記）' : '（未確認產生標記，保留）'}：${relative(ROOT, file)}`)
    }
  }
  for (const { from, to, header } of entries) {
    if (!lstatSync(from).isFile() || lstatSync(from).isSymbolicLink() || (existsSync(to) && lstatSync(to).isSymbolicLink())) {
      throw new Error(`Locale generation requires regular source/output files: ${relative(ROOT, from)}`)
    }
    const expected = generate(readFileSync(from, 'utf8'), { header })
    const current = existsSync(to) ? readFileSync(to, 'utf8') : null
    if (current === expected) continue
    changed++
    const rel = relative(ROOT, to)
    if (check) {
      console.error(`[zh-tw] 過期：${rel}`)
      continue
    }
    mkdirSync(dirname(to), { recursive: true })
    writeFileSync(to, expected, 'utf8')
    console.log(`[zh-tw] 已寫入 ${rel}`)
  }
  if (check) {
    if (changed || unresolvedOrphans) {
      console.error(`\n${changed} 個檔案與 locales/zh 不同步，請執行 node tools/zh-tw/gen-locale.mjs`)
      process.exit(1)
    }
    console.log('[zh-tw] zh-TW 與 zh 同步')
    return
  }
  if (prune && unresolvedOrphans) process.exitCode = 1
  console.log(`\n${changed} 個檔案已更新`)
}

main()
