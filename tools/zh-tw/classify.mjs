// Classify every CJK string literal in backend/**/*.go by the call it sits in,
// so we can size "user-visible message" vs "log field" vs "test name" vs "other".
import { globSync, readFileSync } from 'node:fs'
import { lexGo, lineOf, CJK } from './scan-go.mjs'

const ROOT = process.argv[2] || '.'
const goFiles = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))

const buckets = new Map()
const samples = new Map()

const RULES = [
  ['user-error:infraerrors', /infraerrors\.(New|BadRequest|NotFound|Forbidden|Conflict|TooManyRequests|Internal|ServiceUnavailable|Unauthorized|PaymentRequired|Unprocessable)\w*\(\s*(?:[^,()]*,\s*)?$/],
  ['user-error:errors.New', /errors\.New\($/],
  ['user-error:fmt.Errorf', /fmt\.Errorf\($/],
  ['user-error:response', /(response|resp|httpx|ginx)\.(Error|Fail|BadRequest|Forbidden|NotFound|Abort\w*|JSONError)\w*\([^()]*$/i],
  ['log', /(logger|log|zap|l|lg)\.(Debug|Info|Warn|Error|Fatal|Panic|DPanic|Debugf|Infof|Warnf|Errorf)\w*\($/],
  ['log-field', /zap\.(String|Any|Error|Stringer)\(\s*$/],
  ['test-name', /t\.Run\($/],
  ['test-msg', /(require|assert)\.\w+\([^\n]*$/],
  ['struct-field-desc', /(Name|Title|Label|Desc|Description|Message|Msg|Reason|Summary|Text|Comment|Hint|Placeholder|Note|Notes)\s*:\s*$/],
]

for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const isTest = /_test\.go$/.test(file)
  const { strings } = lexGo(src)
  for (const s of strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const pre = src.slice(Math.max(0, s.start - 200), s.start).replace(/\s+$/, '')
    let label = null
    for (const [name, re] of RULES) { if (re.test(pre)) { label = name; break } }
    if (!label) label = 'other'
    const key = (isTest ? 'TEST | ' : 'PROD | ') + label
    buckets.set(key, (buckets.get(key) || 0) + 1)
    if (!samples.has(key)) samples.set(key, [])
    if (samples.get(key).length < 4) samples.get(key).push(`${file}:${s.line} ${lineOf(src, s.start).trim().slice(0, 130)}`)
  }
}

const sorted = [...buckets.entries()].sort((a, b) => b[1] - a[1])
let total = 0
for (const [k, v] of sorted) total += v
console.log('TOTAL CJK string literals:', total, '\n')
for (const [k, v] of sorted) {
  console.log(`${String(v).padStart(5)}  ${k}`)
  for (const s of samples.get(k)) console.log('        ' + s)
}

// per-directory breakdown of PROD literals
const dirs = new Map()
for (const file of goFiles) {
  if (/_test\.go$/.test(file)) continue
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  const n = strings.filter(s => s.kind !== 'rune' && CJK.test(s.raw)).length
  if (!n) continue
  const d = file.split('/').slice(0, 4).join('/')
  dirs.set(d, (dirs.get(d) || 0) + n)
}
console.log('\n--- PROD CJK literals by dir ---')
for (const [d, n] of [...dirs.entries()].sort((a, b) => b[1] - a[1]).slice(0, 30)) console.log(String(n).padStart(5), d)
