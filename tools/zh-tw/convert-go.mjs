// Surgical zh-CN -> zh-TW conversion for backend/**/*.go STRING LITERALS.
//
// Design invariant: a literal is converted only if it is *produced* (shown to a
// user / logged), never if it is *matched* against text this system does not
// author (external upstreams, historical DB rows).
//
// Not touched at all:
//   - Go comments (kept simplified on purpose: zero user impact, keeps diff reviewable)
//   - migrations/**.sql (applied DDL + seed rows already live in production DBs)
//   - raw string literals that carry SQL
//   - everything in PROTECTED below
import { existsSync, globSync, readFileSync, writeFileSync } from 'node:fs'
import { relative } from 'node:path'
import { lexGo, CJK } from './scan-go.mjs'
import { toTraditional } from './convert.mjs'

const ROOT = process.argv[2] || '.'
const DRY = process.argv.includes('--dry')
const LIST_PROTECTED = process.argv.includes('--list-protected')

// 轉換不是冪等的（文档→文件→檔案），對已經轉過的樹再跑一次會改壞字串。
// 轉換完成後在目標樹寫入標記檔；再次執行時看到標記就拒絕。
// Docker 每次建置都從乾淨的原始碼複製，不會碰到標記；若標記出現在 repo 的
// backend/ 裡，代表有人就地轉換過原始碼，建置會在這裡明確失敗而不是靜默改壞。
const MARKER = `${ROOT}/.zh-tw-converted`
if (!DRY && !LIST_PROTECTED && existsSync(MARKER)) {
  console.error(`convert-go: ${ROOT} 已經轉換過（存在 ${MARKER}），拒絕再次轉換。`)
  console.error('請從乾淨的簡體原始碼重新複製後再執行；若確定要重跑，先刪除該標記檔。')
  process.exit(1)
}

// ---------------------------------------------------------------------------
// PROTECTED: literals that must stay Simplified because they are compared
// against text produced elsewhere. Verified by audit.mjs / audit2.mjs / audit3.mjs.
// ---------------------------------------------------------------------------

// Whole files whose Chinese literals are all upstream-error matchers.
const PROTECTED_FILES = new Set([
  // Every non-comment CJK literal here classifies xAI/Grok upstream error text.
  'internal/service/grok_upstream_failure.go',
  // Hand-edited separately to match BOTH simplified (historical DB rows) and traditional.
  'internal/handler/ops_error_logger.go',
  'cmd/cleanup-ingress-reject-logs/main.go',
  // Their tests pin the historical/simplified wire format.
  'internal/handler/ops_error_logger_test.go',
  'cmd/cleanup-ingress-reject-logs/main_test.go',
  'internal/service/grok_upstream_failure_test.go',
  // 管理員合規確認短語：前端逐字比對、後端 expectedAdminCompliancePhrase 也比對，
  // 轉成繁體會讓簡體介面顯示繁體短語。繁體介面要顯示的短語由前端常數提供
  // （frontend/src/stores/adminCompliance.ts），送回後端時仍用這裡的簡體字串。
  'internal/service/admin_compliance.go',
])

// file -> exact literal contents that must stay Simplified.
const PROTECTED_LITERALS = {
  // EasyPay gateway refund responses. Always Simplified, authored by the gateway.
  'internal/payment/provider/easypay.go': ['订单编号不存在', '订单不存在'],
  // Zhipu/Kimi upstream response body sniffing.
  'internal/service/ratelimit_cn_providers.go': ['余额不足'],
  // Grok upstream text sniffing.
  'internal/service/grok_model_quota_block.go': ['模型'],
  // DingTalk user-extension map key, authored by DingTalk.
  'internal/handler/auth_dingtalk_client.go': ['企业邮箱'],
  // OpenAI images refusal markers, matched against OpenAI upstream text.
  'internal/service/openai_images_responses.go': [
    '安全系统', '安全策略', '安全政策', '内容政策', '内容审核', '违规内容', '不适合生成',
  ],
}

// --list-protected: 印出保護清單（PROTECTED_FILES ∪ PROTECTED_LITERALS 的鍵），
// 每行一個 backend 相對路徑。給上游同步流程用來判斷「上游改到了保護清單的檔案」。
// 不需要 ROOT 參數、不讀寫任何原始碼檔案。
if (LIST_PROTECTED) {
  const all = new Set([...PROTECTED_FILES, ...Object.keys(PROTECTED_LITERALS)])
  for (const f of [...all].sort()) console.log(f)
  process.exit(0)
}

const rel = (f) => f.replaceAll(String.fromCharCode(92), '/').replace(/^\.\//, '')
// Raw strings that carry SQL or Redis Lua. Their Chinese only ever appears in
// `--` comments, so converting buys nothing -- and editing a Lua body changes the
// script's SHA1 (forcing an EVALSHA reload) inside concurrency/session limiting.
// Detect the comment marker itself, not just SQL keywords: these scripts are
// concatenated in fragments and a fragment may carry no keyword at all.
const SQLISH = /\b(SELECT|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|WITH\s+\w+\s+AS|COALESCE)\b/i
const SQL_OR_LUA_COMMENT = /(^|\n)\s*--\s|redis\.call|KEYS\[|ARGV\[/

// ent/ is excluded wholesale:
//  - ent/*.go is generated; ent/schema/*.go .Comment() text is copied into it
//    verbatim, so converting the schema without re-running `go generate` causes drift.
//  - none of that text is user-visible; it is schema documentation.
const EXCLUDED_DIRS = [/^ent\//]

const files = globSync('**/*.go', { cwd: ROOT, nodir: true })
  .filter(f => !EXCLUDED_DIRS.some(re => re.test(rel(f))))
  .map(f => ROOT + '/' + rel(f))

// ---------------------------------------------------------------------------
// Pass 1: production (non-test) files.
// ---------------------------------------------------------------------------
const convertedProd = new Set()   // original Simplified literal contents we converted
const protectedTexts = new Set()  // literal contents we deliberately left Simplified
const report = []

function literalBody(s) {
  return s.kind === 'raw' ? s.raw.slice(1, -1) : s.raw.slice(1, -1)
}

function processFile(file, { testPhase }) {
  // 用 path.relative 取得相對 ROOT 的路徑，並統一成 '/'；先前用正規表示式剝前綴，
  // 在 Windows 絕對路徑（反斜線）下對不上，會讓 PROTECTED 清單整個失效。
  const r = rel(relative(ROOT, file))
  let src
  try { src = readFileSync(file, 'utf8') } catch { return 0 }
  if (!CJK.test(src)) return 0

  if (PROTECTED_FILES.has(r)) {
    const { strings } = lexGo(src)
    for (const s of strings) if (s.kind !== 'rune' && CJK.test(s.raw)) protectedTexts.add(literalBody(s))
    return 0
  }
  const fileProtected = PROTECTED_LITERALS[r] || []

  const { strings } = lexGo(src)
  const edits = []
  for (const s of strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const body = literalBody(s)
    if (fileProtected.includes(body)) { protectedTexts.add(body); continue }
    if (s.kind === 'raw' && (SQLISH.test(body) || SQL_OR_LUA_COMMENT.test(body))) { protectedTexts.add(body); continue }

    if (testPhase) {
      // Only convert a test literal when it is tied to a literal we converted in
      // production, and is not contained in anything we deliberately protected.
      const cjkChars = (body.match(/[㐀-䶿一-鿿]/g) || []).length
      if (cjkChars < 2) continue
      let tied = convertedProd.has(body)
      if (!tied) for (const p of convertedProd) { if (p.includes(body)) { tied = true; break } }
      if (!tied) continue
      let poisoned = false
      for (const p of protectedTexts) { if (p.includes(body) || body.includes(p)) { poisoned = true; break } }
      if (poisoned) continue
    }

    const out = toTraditional(body)
    if (out === body) continue
    edits.push({ start: s.start, end: s.end, from: s.raw, to: s.raw[0] + out + s.raw[s.raw.length - 1] })
    if (!testPhase) convertedProd.add(body)
    report.push(`${r}:${s.line} ${JSON.stringify(body.slice(0, 60))} -> ${JSON.stringify(out.slice(0, 60))}`)
  }
  if (!edits.length) return 0
  let out = ''
  let cur = 0
  for (const e of edits) { out += src.slice(cur, e.start) + e.to; cur = e.end }
  out += src.slice(cur)
  if (!DRY) writeFileSync(file, out, 'utf8')
  return edits.length
}

let prodFiles = 0, prodEdits = 0
for (const f of files) {
  if (/_test\.go$/.test(f)) continue
  const n = processFile(f, { testPhase: false })
  if (n) { prodFiles++; prodEdits += n }
}

let testFiles = 0, testEdits = 0
for (const f of files) {
  if (!/_test\.go$/.test(f)) continue
  const n = processFile(f, { testPhase: true })
  if (n) { testFiles++; testEdits += n }
}

console.log(`prod: ${prodEdits} literals in ${prodFiles} files`)
console.log(`test: ${testEdits} literals in ${testFiles} files`)
console.log(`protected (left Simplified): ${protectedTexts.size} distinct literals`)
if (process.argv.includes('--report')) { console.log('---'); for (const l of report) console.log(l) }
if (!DRY) writeFileSync(MARKER, `converted at ${new Date().toISOString()}\n`, 'utf8')
