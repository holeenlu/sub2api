// Post-conversion safety check.
//
// Failure mode we are hunting: a literal that is still Simplified because it is
// used for MATCHING, whose *producer* we converted to Traditional. That matcher
// would silently stop firing.
//
// Method: collect every Chinese literal still present in backend Go code, split
// into "matcher" (adjacent to a comparison) and "produced". For each matcher,
// check whether its Traditional form appears anywhere as a produced literal.
import { globSync, readFileSync } from 'node:fs'
import { lexGo, lineOf, CJK } from './scan-go.mjs'
import { toTraditional } from './convert.mjs'

const ROOT = process.argv[2] || '.'
const files = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))

const CMP_PRE = [
  /[=!]=$/,
  /(strings|bytes)\.\w+\s*\($/,
  /(strings|bytes)\.\w+\s*\([^()]*,$/,
  /regexp\.(MustCompile|Compile|MatchString)\s*\($/,
  /,$/, // inside a []string{...} keyword list -- handled by the slice check below
]

const matchers = new Map() // literal -> [locations]
const produced = new Map() // literal -> [locations]

for (const file of files) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  for (const s of strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const body = s.kind === 'raw' ? s.raw.slice(1, -1) : s.raw.slice(1, -1)
    const pre = src.slice(Math.max(0, s.start - 200), s.start).replace(/\s+$/, '')
    const ln = lineOf(src, s.start)
    const isCmp =
      /[=!]=$/.test(pre) ||
      /(strings|bytes)\.(Contains|HasPrefix|HasSuffix|EqualFold|Index|Count|Cut|Trim\w*|Split\w*)\s*\(([^()]*,)?$/.test(pre) ||
      /regexp\.(MustCompile|Compile|MatchString)\s*\($/.test(pre) ||
      /^\s*case\s/.test(ln)
    const bucket = isCmp ? matchers : produced
    if (!bucket.has(body)) bucket.set(body, [])
    bucket.get(body).push(`${file}:${s.line}`)
  }
}

// Chinese keyword slices: treat every CJK entry of a []string{...} as a matcher too.
for (const file of files) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  const openRe = /\[\]string\s*\{/g
  let m
  while ((m = openRe.exec(src))) {
    let depth = 0, i = m.index + m[0].length - 1
    const start = i
    for (; i < src.length; i++) { if (src[i] === '{') depth++; else if (src[i] === '}') { depth--; if (!depth) break } }
    for (const s of strings) {
      if (s.start > start && s.end < i && s.kind !== 'rune' && CJK.test(s.raw)) {
        const body = s.raw.slice(1, -1)
        if (!matchers.has(body)) matchers.set(body, [])
        matchers.get(body).push(`${file}:${s.line}`)
      }
    }
  }
}

const simplifiedMatchers = [...matchers.entries()].filter(([k]) => toTraditional(k) !== k)
console.log('Chinese matcher literals still Simplified:', simplifiedMatchers.length)

let risky = 0
for (const [lit, locs] of simplifiedMatchers) {
  const trad = toTraditional(lit)
  // Does the Traditional form now exist as something this system produces?
  const hits = []
  for (const [p, plocs] of produced) if (p.includes(trad)) hits.push(...plocs)
  const alsoMatched = matchers.has(trad)
  if (hits.length) {
    risky++
    console.log(`\n[RISK] matcher ${JSON.stringify(lit)}  (traditional form is produced ${hits.length}x)`)
    console.log(`   matcher at : ${[...new Set(locs)].slice(0, 4).join(', ')}`)
    console.log(`   produced at: ${[...new Set(hits)].slice(0, 4).join(', ')}`)
    console.log(`   traditional variant also matched? ${alsoMatched ? 'YES (ok)' : 'NO  <-- needs dual match'}`)
  }
}
console.log(`\nmatchers whose traditional form is produced somewhere: ${risky}`)
