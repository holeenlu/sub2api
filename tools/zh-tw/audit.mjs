import { globSync, readFileSync } from 'node:fs'
import { lexGo, lineOf, CJK } from './scan-go.mjs'

const ROOT = process.argv[2] || '.'
const files = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))

const CMP_FUNCS = /(strings|bytes)\.(Contains|ContainsAny|ContainsRune|HasPrefix|HasSuffix|EqualFold|Index|LastIndex|IndexAny|Count|Trim|TrimLeft|TrimRight|TrimPrefix|TrimSuffix|Split|SplitN|SplitAfter|Cut|CutPrefix|CutSuffix|Replace|ReplaceAll|Fields|Title|Compare|Equal)\s*\($/
const RE_FUNCS = /regexp\.(MustCompile|Compile|MatchString|QuoteMeta)\s*\($/
const SLICES = /(slices|lo)\.(Contains|ContainsFunc|Index|IndexOf)\s*\([^()]*,\s*$/

const findings = []
let totalCJKStrings = 0

for (const file of files) {
  let src
  try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  for (const s of strings) {
    if (s.kind === 'rune') continue
    if (!CJK.test(s.raw)) continue
    totalCJKStrings++
    // preceding non-whitespace context (up to 120 chars, whitespace-collapsed)
    const preRaw = src.slice(Math.max(0, s.start - 160), s.start)
    const pre = preRaw.replace(/\s+$/, '')
    const post = src.slice(s.end, s.end + 60).replace(/^\s+/, '')
    const reasons = []

    if (/[=!]=$/.test(pre)) reasons.push('EQ-before')
    if (/^[=!]=/.test(post)) reasons.push('EQ-after')
    if (CMP_FUNCS.test(pre)) reasons.push('strings-fn:' + pre.match(CMP_FUNCS)[0].trim())
    if (RE_FUNCS.test(pre)) reasons.push('regexp')
    if (SLICES.test(pre)) reasons.push('slices.Contains')
    // arg position 2+ of a strings.X( call: e.g. strings.Contains(msg, "..")
    const m2 = pre.match(/(strings|bytes)\.(\w+)\s*\([^()]*,$/)
    if (m2) reasons.push('strings-fn-arg2:' + m2[1] + '.' + m2[2])
    // case clause
    const ln = lineOf(src, s.start)
    if (/^\s*case\s/.test(ln)) reasons.push('switch-case')
    // map key in composite literal: "..." :
    if (/^:/.test(post) && !/^::/.test(post)) reasons.push('map-key-or-struct')
    // map/array index lookup: x["..."]
    if (/\[$/.test(pre) && /^\]/.test(post)) reasons.push('index-lookup')
    // regexp in raw backtick used later
    if (s.kind === 'raw' && /[$^|()[\]]/.test(s.raw) && /regexp|MustCompile/.test(preRaw)) reasons.push('regexp-raw')

    if (reasons.length) {
      findings.push({ file, line: s.line, reasons, text: s.raw.slice(0, 90), ctx: ln.trim().slice(0, 160) })
    }
  }
}

console.log('CJK string literals total:', totalCJKStrings)
console.log('comparison-risk findings:', findings.length)
console.log('---')
const byReason = {}
for (const f of findings) for (const r of f.reasons) byReason[r.split(':')[0]] = (byReason[r.split(':')[0]] || 0) + 1
console.log(JSON.stringify(byReason, null, 1))
console.log('---')
for (const f of findings) console.log(`${f.file}:${f.line} [${f.reasons.join(',')}] ${f.ctx}`)
