// Audit Chinese literals that bypass locale messages, including Vue templates.
import { readFileSync, globSync } from 'node:fs'
import { createRequire } from 'node:module'
import { resolve, dirname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const require = createRequire(resolve(ROOT, 'frontend/package.json'))
const ts = require('typescript')
const vue = require('vue/compiler-sfc')
const dom = createRequire(require.resolve('vue/compiler-sfc'))('@vue/compiler-dom')
const CJK = /[\u3400-\u9fff]/

// Protocol acknowledgement phrases must match the backend exactly. Language
// selector names are intentionally written in their native languages.
const ALLOWED = {
  'stores/adminCompliance.ts': new Set([
    '我已阅读、理解并同意 ', ' 部署与运营合规承诺',
    '我已閱讀、理解並同意 ', ' 部署與營運合規承諾',
    ' のデプロイおよび運用コンプライアンス誓約を読み、理解し、同意しました',
  ]),
  'i18n/index.ts': new Set(['简体中文', '繁體中文', '日本語']),
  'config/brand.ts': new Set(['/PAYMENT_CN.md#支持的支付方式']),
}

export function auditUI(root = ROOT) {
  const hits = []
  let files = 0
  function record(file, line, text) {
    if (CJK.test(text) && !ALLOWED[file]?.has(text)) hits.push(`${file}:${line} ${JSON.stringify(text.slice(0, 140))}`)
  }
  function code(source, file, offset = 0) {
    if (typeof source !== 'string') return
    const ast = ts.createSourceFile(`${file}.ts`, source, ts.ScriptTarget.Latest, true)
    function visit(node) {
      if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) ||
          ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) {
        record(file, source.slice(0, node.getStart(ast)).split('\n').length + offset, node.text)
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  for (const file of globSync('**/*.{vue,ts}', { cwd: resolve(root, 'frontend/src') })) {
    if (/\/(__tests__|locales)\/|\.(spec|test)\.ts$/.test(file)) continue
    files++
    const source = readFileSync(resolve(root, 'frontend/src', file), 'utf8')
    if (file.endsWith('.ts')) { code(source, file); continue }
    const { descriptor } = vue.parse(source)
    for (const block of [descriptor.script, descriptor.scriptSetup]) {
      if (block) code(block.content, file, block.loc.start.line - 1)
    }
    if (!descriptor.template) continue
    const offset = descriptor.template.loc.start.line - 1
    function template(node) {
      if (node.type === 2) record(file, node.loc.start.line + offset, node.content)
      if (node.type === 5) code(node.content.content, file, node.loc.start.line + offset - 1)
      if (node.type === 1) for (const prop of node.props) {
        if (prop.type === 6 && prop.value) record(file, prop.loc.start.line + offset, prop.value.content)
        if (prop.type === 7 && prop.exp) code(prop.exp.content, file, prop.loc.start.line + offset - 1)
      }
      for (const child of node.children || []) template(child)
    }
    template(dom.parse(descriptor.template.content))
  }
  return { files, hits: [...new Set(hits)].sort() }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { files, hits } = auditUI()
  console.log(`UI audit: ${files} source files, ${hits.length} untranslated Chinese literals`)
  if (hits.length) { console.error(hits.join('\n')); process.exitCode = 1 }
}
