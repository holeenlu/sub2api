import test from 'node:test'
import assert from 'node:assert/strict'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'

const HERE = dirname(fileURLToPath(import.meta.url))
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'sub2api-locale-cli-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const tools = join(root, 'tools/zh-tw')
  mkdirSync(tools, { recursive: true })
  for (const name of ['gen-locale.mjs', 'audit-locale.mjs', 'convert.mjs']) copyFileSync(join(HERE, name), join(tools, name))
  symlinkSync(join(HERE, 'node_modules'), join(tools, 'node_modules'), 'dir')
  const put = (relative, content) => { const path = join(root, relative); mkdirSync(dirname(path), { recursive: true }); writeFileSync(path, content) }
  put('frontend/src/i18n/locales/zh/index.ts', "export default { common: { title: '配置' } }\n")
  put('frontend/src/features/catalog/locales/zh/index.ts', "export default { title: '请求' }\n")
  put('frontend/src/features/site/docs/content/zh/example.md', '# 配置\n')
  put('frontend/src/features/site/docs/content/zh-TW/example.md', 'DOCS_SENTINEL\n')
  put('docs/legal/admin-compliance.zh.md', '# 配置\n')
  put('docs/legal/admin-compliance.zh-TW.md', 'LEGAL_SENTINEL\n')
  const cli = (...args) => spawnSync(process.execPath, [join(tools, 'gen-locale.mjs'), ...args], { cwd: tmpdir(), encoding: 'utf8' })
  return { root, tools, put, cli, read: (path) => readFileSync(join(root, path), 'utf8') }
}

test('locales-only generates both owners without touching docs or legal', (t) => {
  const f = fixture(t)
  let result = f.cli('--locales-only')
  assert.equal(result.status, 0, result.stderr)
  for (const path of ['frontend/src/i18n/locales/zh-TW/index.ts', 'frontend/src/features/catalog/locales/zh-TW/index.ts']) assert.match(f.read(path), /由 tools\/zh-tw\/gen-locale\.mjs/)
  assert.equal(f.read('frontend/src/features/site/docs/content/zh-TW/example.md'), 'DOCS_SENTINEL\n')
  assert.equal(f.read('docs/legal/admin-compliance.zh-TW.md'), 'LEGAL_SENTINEL\n')
  assert.equal(f.cli('--locales-only', '--check').status, 0)
  assert.equal(f.cli('--check').status, 1)
  result = f.cli()
  assert.equal(result.status, 0, result.stderr)
  assert.notEqual(f.read('frontend/src/features/site/docs/content/zh-TW/example.md'), 'DOCS_SENTINEL\n')
  assert.notEqual(f.read('docs/legal/admin-compliance.zh-TW.md'), 'LEGAL_SENTINEL\n')
})

test('check detects old output and explicit pruning preserves unverified files', (t) => {
  const f = fixture(t)
  assert.equal(f.cli('--locales-only').status, 0)
  unlinkSync(join(f.root, 'frontend/src/features/catalog/locales/zh/index.ts'))
  const orphan = 'frontend/src/features/catalog/locales/zh-TW/index.ts'
  const check = f.cli('--locales-only', '--check')
  assert.equal(check.status, 1)
  assert.ok(check.stderr.includes(orphan))
  assert.ok(existsSync(join(f.root, orphan)))
  const manual = 'frontend/src/i18n/locales/zh-TW/manual.ts'
  f.put(manual, 'export default { retained: true }\n')
  const prune = f.cli('--locales-only', '--prune-generated-orphans')
  assert.equal(prune.status, 1)
  assert.equal(existsSync(join(f.root, orphan)), false)
  assert.equal(f.read(manual), 'export default { retained: true }\n')
  assert.equal(f.read('docs/legal/admin-compliance.zh-TW.md'), 'LEGAL_SENTINEL\n')
})

test('check never permits pruning and audit includes feature-owned locale files', (t) => {
  const f = fixture(t)
  assert.notEqual(f.cli('--check', '--prune-generated-orphans').status, 0)
  assert.equal(existsSync(join(f.root, 'frontend/src/i18n/locales/zh-TW/index.ts')), false)
  f.put('frontend/src/features/catalog/locales/zh-TW/index.ts', "export default { value: '當前' }\n")
  const audit = spawnSync(process.execPath, [join(f.tools, 'audit-locale.mjs'), '--all'], { encoding: 'utf8' })
  assert.equal(audit.status, 0, audit.stderr)
  assert.ok(audit.stdout.includes('frontend/src/features/catalog/locales/zh-TW/index.ts'))
})
