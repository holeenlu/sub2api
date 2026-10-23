// Second-pass audit: indirect comparison risks that audit.mjs (adjacency-based) cannot see.
//  A) Chinese string bound to a named const/var, where that name is later used in a comparison.
//  B) Chinese used as a map key in a composite literal (declaration side).
//  C) Chinese passed to ent/schema Default(...) or persisted-looking setters.
//  D) Chinese inside SQL (migrations + inline Go SQL strings), esp. WHERE / VALUES.
import { globSync, readFileSync } from 'node:fs'
import { lexGo, lineOf, CJK } from './scan-go.mjs'

const ROOT = process.argv[2] || '.'
const goFiles = globSync('**/*.go', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))

const out = (title, rows) => {
  console.log('\n===== ' + title + ' (' + rows.length + ') =====')
  for (const r of rows) console.log(r)
}

// ---------- A) named constants holding Chinese ----------
const chineseConsts = new Map() // name -> [file:line, text]
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  // Only genuine declarations:  const X = "中文" / var X = "中文" / var X string = "中文"
  // and entries inside a `const ( ... )` block. NOT `:=` locals.
  const re = /(?:^|\n)\s*(?:const|var)\s+([A-Za-z_]\w*)(?:\s+string)?\s*=\s*("(?:[^"\\\n]|\\.)*")/g
  let m
  while ((m = re.exec(src))) {
    if (!CJK.test(m[2])) continue
    const line = src.slice(0, m.index).split('\n').length
    if (!chineseConsts.has(m[1])) chineseConsts.set(m[1], [])
    chineseConsts.get(m[1]).push(`${file}:${line} ${m[1]} = ${m[2].slice(0, 60)}`)
  }
  // grouped const/var blocks
  const blockRe = /\n(const|var)\s*\(([\s\S]*?)\n\)/g
  let b
  while ((b = blockRe.exec(src))) {
    const body = b[2]
    const er = /\n\s*([A-Za-z_]\w*)(?:\s+string)?\s*=\s*("(?:[^"\\\n]|\\.)*")/g
    let e
    while ((e = er.exec(body))) {
      if (!CJK.test(e[2])) continue
      const line = src.slice(0, b.index + b[0].indexOf(e[0])).split('\n').length
      if (!chineseConsts.has(e[1])) chineseConsts.set(e[1], [])
      const s = `${file}:${line} ${e[1]} = ${e[2].slice(0, 60)}`
      if (!chineseConsts.get(e[1]).includes(s)) chineseConsts.get(e[1]).push(s)
    }
  }
}

// where are those names compared?
const cmpUses = []
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  for (const name of chineseConsts.keys()) {
    // Skip generic lowercase names (message/body/input/mode/...) that collide with
    // ubiquitous local variables and drown the signal. Go const/var identifiers that
    // matter here are CamelCase.
    if (name.length < 4 || !/[A-Z]/.test(name)) continue
    const re = new RegExp('(?:[=!]=\\s*(?:\\w+\\.)?' + name + '\\b)|(?:\\b(?:\\w+\\.)?' + name + '\\s*[=!]=)|(?:case\\s+(?:\\w+\\.)?' + name + '\\b)|(?:(?:Contains|HasPrefix|HasSuffix|EqualFold)\\([^)\\n]*\\b' + name + '\\b)', 'g')
    let m
    while ((m = re.exec(src))) {
      const line = src.slice(0, m.index).split('\n').length
      cmpUses.push(`${file}:${line} [const ${name}] ${lineOf(src, m.index).trim().slice(0, 150)}`)
    }
  }
}
out('A) Chinese-valued identifiers used in a comparison', cmpUses)
console.log('   (total distinct Chinese-valued identifiers scanned: ' + chineseConsts.size + ')')

// ---------- B) Chinese as map key in composite literal ----------
const mapKeys = []
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  for (const s of strings) {
    if (s.kind === 'rune' || !CJK.test(s.raw)) continue
    const post = src.slice(s.end, s.end + 40)
    const pre = src.slice(Math.max(0, s.start - 400), s.start)
    if (!/^\s*:/.test(post)) continue
    if (/case\s[^\n]*$/.test(pre)) continue // switch-case colon, not a map key
    // require a map/composite-literal opener reasonably nearby
    if (!/map\[string\][^\n]*\{|\{\s*$|,\s*$/.test(pre.replace(/\s+$/, ''))) continue
    mapKeys.push(`${file}:${s.line} ${lineOf(src, s.start).trim().slice(0, 150)}`)
  }
}
out('B) Chinese map/composite-literal keys (declaration side)', mapKeys)

// ---------- C) ent schema defaults / persisted values ----------
const defaults = []
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const re = /\.(Default|DefaultFunc|Comment|StructTag|Value|Enum|Values|NamedValues)\s*\(\s*("(?:[^"\\\n]|\\.)*")/g
  let m
  while ((m = re.exec(src))) {
    if (!CJK.test(m[2])) continue
    const line = src.slice(0, m.index).split('\n').length
    defaults.push(`${file}:${line} .${m[1]}(${m[2].slice(0, 70)})`)
  }
}
out('C) ent/schema Default()/Enum()/Comment() with Chinese', defaults)

// ---------- D) Chinese inside SQL ----------
const sqlHits = []
const sqlFiles = globSync('**/*.sql', { cwd: ROOT, nodir: true }).map(f => ROOT + '/' + f.replaceAll(String.fromCharCode(92), '/'))
for (const file of sqlFiles) {
  const src = readFileSync(file, 'utf8')
  if (!CJK.test(src)) continue
  const lines = src.split('\n')
  lines.forEach((l, idx) => {
    if (!CJK.test(l)) return
    const stripped = l.replace(/--.*$/, '')
    if (!CJK.test(stripped)) return // comment-only line
    sqlHits.push(`${file}:${idx + 1} ${l.trim().slice(0, 160)}`)
  })
}
out('D) Chinese in SQL outside of -- comments (migrations)', sqlHits)

// inline SQL in Go
const goSql = []
for (const file of goFiles) {
  let src; try { src = readFileSync(file, 'utf8') } catch { continue }
  if (!CJK.test(src)) continue
  const { strings } = lexGo(src)
  for (const s of strings) {
    if (!CJK.test(s.raw)) continue
    if (!/\b(SELECT|INSERT|UPDATE|DELETE|WHERE|VALUES)\b/i.test(s.raw)) continue
    goSql.push(`${file}:${s.line} ${s.raw.replace(/\s+/g, ' ').slice(0, 160)}`)
  }
}
out('D2) Chinese inside inline SQL string literals in Go', goSql)
