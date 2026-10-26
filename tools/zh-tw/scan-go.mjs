// Go-aware lexer: extracts string literals & comments with byte positions.
// Deliberately simple but correct for the constructs that appear in this repo:
// // line comments, /* */ block comments, "interpreted", `raw`, 'rune' literals.
import { readFileSync } from 'node:fs'

export const CJK = /[㐀-䶿一-鿿豈-﫿]/

const BS = String.fromCharCode(92) // backslash

export function lexGo(src) {
  const strings = []
  const comments = []
  let i = 0
  const n = src.length
  let line = 1
  while (i < n) {
    const c = src[i]
    if (c === '\n') { line++; i++; continue }

    if (c === '/' && src[i + 1] === '/') {
      const start = i
      while (i < n && src[i] !== '\n') i++
      comments.push({ start, end: i, line, block: false })
      continue
    }
    if (c === '/' && src[i + 1] === '*') {
      const start = i, sl = line
      i += 2
      while (i < n && !(src[i] === '*' && src[i + 1] === '/')) { if (src[i] === '\n') line++; i++ }
      i += 2
      comments.push({ start, end: i, line: sl, block: true })
      continue
    }
    if (c === '"') {
      const start = i, sl = line
      i++
      while (i < n) {
        if (src[i] === BS) { i += 2; continue }
        if (src[i] === '"') { i++; break }
        if (src[i] === '\n') { line++; i++; break } // unterminated -> bail
        i++
      }
      strings.push({ start, end: i, line: sl, kind: 'interp', raw: src.slice(start, i) })
      continue
    }
    if (c === '`') {
      const start = i, sl = line
      i++
      while (i < n && src[i] !== '`') { if (src[i] === '\n') line++; i++ }
      i++
      strings.push({ start, end: i, line: sl, kind: 'raw', raw: src.slice(start, i) })
      continue
    }
    if (c === "'") {
      const start = i
      i++
      while (i < n) {
        if (src[i] === BS) { i += 2; continue }
        if (src[i] === "'") { i++; break }
        if (src[i] === '\n') { i++; break }
        i++
      }
      strings.push({ start, end: i, line, kind: 'rune', raw: src.slice(start, i) })
      continue
    }
    i++
  }
  return { strings, comments }
}

export function lineOf(src, idx) {
  const s = src.lastIndexOf('\n', idx - 1) + 1
  let e = src.indexOf('\n', idx)
  if (e < 0) e = src.length
  return src.slice(s, e)
}

export function readSrc(f) { return readFileSync(f, 'utf8') }
