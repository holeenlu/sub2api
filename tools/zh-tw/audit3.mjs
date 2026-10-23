// Third-pass: Chinese literals living inside []string{...} / map[...]... literals.
// These are the classic "keyword list fed to strings.Contains in a loop" shape that
// adjacency scanning cannot see. Every hit needs a human read.
import { globSync, readFileSync } from 'node:fs'
import { lexGo, CJK } from './scan-go.mjs'

const ROOT = process.argv[2] || '.'
const goFiles = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))

const hits = []
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  // Find []string{ ... } / []T{ ... } / map[string]X{ ... } spans and report CJK literals inside.
  const openRe = /(\[\]string|\[\d*\]string|map\[string\]\w+|map\[string\]\[\]string)\s*\{/g
  let m
  while ((m = openRe.exec(src))) {
    // find matching close brace
    let depth = 0, i = m.index + m[0].length - 1
    const start = i
    for (; i < src.length; i++) {
      if (src[i] === '{') depth++
      else if (src[i] === '}') { depth--; if (depth === 0) break }
    }
    const end = i
    if (end <= start) continue
    const inside = strings.filter(s => s.start > start && s.end < end && s.kind !== 'rune' && CJK.test(s.raw))
    if (!inside.length) continue
    const lineNo = src.slice(0, m.index).split('\n').length
    // context: the ~2 lines before the literal (var name)
    const ctxStart = src.lastIndexOf('\n', src.lastIndexOf('\n', m.index - 1) - 1) + 1
    const ctx = src.slice(ctxStart, m.index + m[0].length).replace(/\s+/g, ' ').trim().slice(-140)
    hits.push({ file, lineNo, ctx, vals: inside.map(s => s.raw).slice(0, 12), n: inside.length })
  }
}

console.log('=== CJK literals inside []string{} / map[string]X{} composite literals ===')
console.log('spans:', hits.length, '\n')
for (const h of hits) {
  console.log(`${h.file}:${h.lineNo}  (${h.n} CJK entries)`)
  console.log(`   ctx: ${h.ctx}`)
  console.log(`   vals: ${h.vals.join(', ').slice(0, 300)}`)
  console.log('')
}
