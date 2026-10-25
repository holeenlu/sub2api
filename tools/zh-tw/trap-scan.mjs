// Hunt the classic zh-CN -> zh-TW one-to-many traps that OpenCC's phrase
// dictionary can miss when the simplified char is ALSO a valid traditional char
// (so nothing "looks" unconverted): 后/後, 里/裡, 只/隻, 面/麵, 发/發, 干/幹,
// 松/鬆, 系/係, 板/闆, 表/錶, 台/臺, 谷/穀, 制/製, 划/劃, 复/複, 卜/蔔.
//
// Reports every occurrence inside a Go string literal that ALSO contains
// traditional-converted text, i.e. strings we touched.
import { globSync, readFileSync } from 'node:fs'
import { lexGo, CJK } from './scan-go.mjs'

const ROOT = process.argv[2] || '.'
const SEP = String.fromCharCode(92)
const files = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => f.replaceAll(SEP, '/'))

// char -> the traditional form it usually should have become in these contexts
const TRAPS = {
  '后': '後 (after/behind)  — 后 only for 皇后/太后',
  '里': '裡 (inside)        — 里 only for 公里/鄰里',
  '只': '隻 (counter)       — 只 only for "only"',
  '面': '麵 (noodles)       — 面 fine for surface/side',
  '发': '發/髮  (unconverted simplified!)',
  '干': '幹/乾  — 干 only for 干預/干擾',
  '松': '鬆 (loose)         — 松 only for pine',
  '系': '係/繫              — 系 fine for 系統/科系',
  '板': '闆 (boss)          — 板 fine for board',
  '表': '錶 (watch)         — 表 fine for table/form',
  '谷': '穀 (grain)',
  '制': '製 (manufacture)   — 制 fine for 制度/控制',
  '划': '劃 (plan/mark)     — 划 only for rowing',
  '复': '複/復/覆 (unconverted simplified!)',
}

const hits = []
for (const f of files) {
  const src = readFileSync(ROOT + '/' + f, 'utf8')
  if (!CJK.test(src)) continue
  for (const s of lexGo(src).strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const b = s.raw.slice(1, -1)
    for (const [ch, note] of Object.entries(TRAPS)) {
      if (!b.includes(ch)) continue
      hits.push({ f, line: s.line, ch, note, text: b.slice(0, 90) })
    }
  }
}

const byChar = {}
for (const h of hits) (byChar[h.ch] ||= []).push(h)
for (const [ch, list] of Object.entries(byChar)) {
  console.log(`\n===== ${ch} -> ${TRAPS[ch]}   (${list.length}) =====`)
  for (const h of list) console.log(`  ${h.f}:${h.line}  ${JSON.stringify(h.text)}`)
}
