// Final review aid: every production (non-test, non-ent) Go string literal that
// is still Simplified after conversion. Each one should be an intentional
// exclusion (upstream-matcher / historical-log matcher).
import { globSync, readFileSync } from 'node:fs'
import { lexGo, CJK } from './scan-go.mjs'
import { toTraditional } from './convert.mjs'

const ROOT = process.argv[2] || '.'
const SEP = String.fromCharCode(92)
const files = globSync('**/*.go', { cwd: ROOT, nodir: true })
  .map(f => f.replaceAll(SEP, '/'))
  .filter(f => !f.endsWith('_test.go') && !f.startsWith('ent/'))

const out = []
for (const f of files) {
  const src = readFileSync(ROOT + '/' + f, 'utf8')
  if (!CJK.test(src)) continue
  for (const s of lexGo(src).strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const b = s.raw.slice(1, -1)
    if (toTraditional(b) === b) continue
    out.push(`${f}:${s.line}  ${JSON.stringify(b.slice(0, 80))}`)
  }
}
console.log('PROD literals still Simplified: ' + out.length)
for (const l of out) console.log('  ' + l)
