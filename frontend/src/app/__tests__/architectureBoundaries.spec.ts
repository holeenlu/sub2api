import { createHash } from 'node:crypto'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import * as ts from 'typescript'
import { parse as parseVue } from 'vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

interface Edge {
  from: string
  line: number
  specifier: string
  to: string
  kind: string
  typeOnly: boolean
  symbols: string[]
  routePage: boolean
  targetPublic: boolean
}
interface Finding extends Edge { rule: string }
interface Effect { file: string; line: number; callee: string; kind: string; trail: string[]; expression: string }
interface State { file: string; line: number; name: string; initializer: string; mutableBinding: boolean }
interface TypeShape { file: string; line: number; name: string; signature: string; members: number; generic: boolean }
interface ModuleInfo { exportedTypes: Set<string>; shapes: TypeShape[] }
interface Chunk { content: string; offset: number; setup: boolean; kind: ts.ScriptKind }
interface RootModule { file: string; runtimeReachable: boolean; path: Edge[]; runtimeInbound: Edge[]; typeInbound: Edge[] }
interface TypeContract extends Edge { contract: string }
export interface ArchitectureAudit {
  sourceHashes: Record<string, string>
  moduleCount: number
  edgeCount: number
  violations: Finding[]
  typeContracts: TypeContract[]
  wildcardExports: { file: string; line: number; specifier: string }[]
  unresolvedImports: Edge[]
  unresolvedDynamicImports: { file: string; line: number; expression: string }[]
  globImports: { file: string; line: number; pattern: string; matched: number }[]
  moduleState: State[]
  eagerEffects: Effect[]
  technicalPreferenceReads: Effect[]
  sharedDTOCopies: { shared: TypeShape; owner: TypeShape }[]
  sharedTypes: TypeShape[]
  rootModules: RootModule[]
}

// i18n is an explicitly retained technical module; assets and declarations are
// not retired buckets. These names come from FRONTEND_ARCHITECTURE's migration.
const retiredRoots = new Set(['api', 'stores', 'types', 'utils', 'components', 'composables', 'views', 'router', 'constants', 'content'])
const testOrEvidence = (file: string) => /(^|\/)(__tests__|tests|\.local|__fixtures__)(\/|$)|\.(spec|test)\.[cm]?[jt]sx?$/.test(file)
const feature = (file: string) => file.startsWith('features/') ? file.split('/')[1] : null
// Feature-root TS facets publish API/ports/panels/routes. Implementation entry
// names do not become public just because a small domain puts them at its root.
const publicEntry = (file: string) => /^features\/[^/]+\/[^/]+\.ts$/.test(file)
  && !/(^|\/)(model|store|helpers|queries|commands)\.ts$/.test(file)
const hasExport = (node: ts.Node) => ts.canHaveModifiers(node)
  && !!ts.getModifiers(node)?.some(modifier => modifier.kind === ts.SyntaxKind.ExportKeyword)

function globExpression(pattern: string): RegExp {
  let output = '^'
  for (let index = 0; index < pattern.length; index++) {
    const char = pattern[index]
    if (char === '*' && pattern[index + 1] === '*') {
      index++
      if (pattern[index + 1] === '/') { output += '(?:.*/)?'; index++ } else output += '.*'
    } else if (char === '*') output += '[^/]*'
    else if (char === '?') output += '[^/]'
    else if (char === '{') output += '(?:'
    else if (char === '}') output += ')'
    else if (char === ',') output += '|'
    else output += /[.()+^$|\\[\]]/.test(char) ? '\\' + char : char
  }
  return new RegExp(output + '$')
}

export function auditProduction(sourceRoot: string): ArchitectureAudit {
  const allFiles: string[] = []
  function walk(directory: string): void {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const absolute = join(directory, entry.name)
      const file = relative(sourceRoot, absolute).split(sep).join('/')
      if (testOrEvidence(file)) continue
      if (entry.isDirectory()) walk(absolute)
      else allFiles.push(file)
    }
  }
  walk(sourceRoot)
  const files = allFiles.filter(file => /\.(?:[cm]?[jt]sx?|vue)$/.test(file))
  const allSet = new Set(allFiles)
  function localPath(specifier: string, from: string): string | null {
    const spec = specifier.split('?')[0]
    if (spec.startsWith('@/')) return spec.slice(2)
    if (!spec.startsWith('.')) return null
    const absolute = resolve(sourceRoot, dirname(from), spec)
    if (!absolute.startsWith(sourceRoot + sep)) return null
    return relative(sourceRoot, absolute).split(sep).join('/')
  }
  function resolveTarget(specifier: string, from: string): string | null {
    const candidate = localPath(specifier, from)
    if (candidate === null) return null
    for (const suffix of ['', '.ts', '.tsx', '.mts', '.cts', '.js', '.jsx', '.vue', '.json', '/index.ts', '/index.js']) {
      if (allSet.has(candidate + suffix)) return candidate + suffix
    }
    return candidate
  }

  const modules = new Map<string, ModuleInfo>()
  const edges: Edge[] = []
  const sourceHashes: Record<string, string> = {}
  const wildcardExports: ArchitectureAudit['wildcardExports'] = []
  const unresolvedDynamicImports: ArchitectureAudit['unresolvedDynamicImports'] = []
  const globImports: ArchitectureAudit['globImports'] = []
  const moduleState: State[] = []
  const eagerEffects: Effect[] = []
  const printer = ts.createPrinter({ removeComments: true })

  for (const file of files) {
    const text = readFileSync(join(sourceRoot, file), 'utf8')
    sourceHashes[file] = createHash('sha256').update(text).digest('hex')
    let chunks: Chunk[] = [{ content: text, offset: 0, setup: false, kind: /\.[jt]sx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS }]
    if (file.endsWith('.vue')) {
      const parsed = parseVue(text, { filename: file })
      if (parsed.errors.length) throw new Error(file + ': ' + parsed.errors.map(String).join('; '))
      const descriptor = parsed.descriptor
      chunks = [descriptor.script, descriptor.scriptSetup].flatMap(block => block ? [{
        content: block.content, offset: block.loc.start.offset, setup: block === descriptor.scriptSetup,
        kind: block.lang === 'tsx' ? ts.ScriptKind.TSX : ts.ScriptKind.TS
      }] : [])
    }
    const info: ModuleInfo = { exportedTypes: new Set(), shapes: [] }
    for (const chunk of chunks) {
      const sf = ts.createSourceFile(file, chunk.content, ts.ScriptTarget.Latest, true, chunk.kind)
      const line = (node: ts.Node) => text.slice(0, chunk.offset + node.getStart(sf)).split('\n').length
      const edge = (node: ts.Node, specifier: string, kind: string, typeOnly: boolean, symbols: string[] = []): void => {
        const target = resolveTarget(specifier, file)
        if (target === null) return
        edges.push({ from: file, line: line(node), specifier, to: target, kind, typeOnly, symbols,
          routePage: /(^|\/)router\/|(^|\/)routes\.ts$/.test(file) || /\/pages\/.*\.vue$/.test(target), targetPublic: publicEntry(target) })
      }
      for (const node of sf.statements) {
        if ((ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) && hasExport(node)) {
          info.exportedTypes.add(node.name.text)
          const signature = printer.printNode(ts.EmitHint.Unspecified, node, sf).replace(node.name.text, '$TYPE').replace(/\s+/g, '')
          info.shapes.push({ file, line: line(node), name: node.name.text, signature,
            members: ts.isInterfaceDeclaration(node) ? node.members.length : 0, generic: !!node.typeParameters?.length })
        }
        if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
          const clause = node.importClause
          if (!clause) edge(node, node.moduleSpecifier.text, 'side-effect-import', false)
          else {
            const values: string[] = [], types: string[] = []
            if (clause.name) (clause.isTypeOnly ? types : values).push('default')
            const bindings = clause.namedBindings
            if (bindings && ts.isNamespaceImport(bindings)) (clause.isTypeOnly ? types : values).push('*')
            if (bindings && ts.isNamedImports(bindings)) for (const item of bindings.elements) {
              (clause.isTypeOnly || item.isTypeOnly ? types : values).push(item.propertyName?.text ?? item.name.text)
            }
            if (values.length) edge(node, node.moduleSpecifier.text, 'import', false, values)
            if (types.length) edge(node, node.moduleSpecifier.text, 'import', true, types)
          }
        }
        if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
          const clause = node.exportClause
          if (!clause) {
            edge(node, node.moduleSpecifier.text, 'export-star', node.isTypeOnly, ['*'])
            wildcardExports.push({ file, line: line(node), specifier: node.moduleSpecifier.text })
          } else if (ts.isNamedExports(clause)) {
            const values: string[] = [], types: string[] = []
            for (const item of clause.elements) (node.isTypeOnly || item.isTypeOnly ? types : values).push(item.propertyName?.text ?? item.name.text)
            if (values.length) edge(node, node.moduleSpecifier.text, 'reexport', false, values)
            if (types.length) edge(node, node.moduleSpecifier.text, 'reexport', true, types)
          } else {
            edge(node, node.moduleSpecifier.text, 'namespace-export', node.isTypeOnly, ['*'])
            wildcardExports.push({ file, line: line(node), specifier: node.moduleSpecifier.text })
          }
        }
        if (!chunk.setup && ts.isVariableStatement(node)) for (const declaration of node.declarationList.declarations) {
          const initializer = declaration.initializer
          if (!initializer || ts.isArrowFunction(initializer) || ts.isFunctionExpression(initializer)) continue
          const mutable = !(node.declarationList.flags & ts.NodeFlags.Const)
          if (mutable || ts.isNewExpression(initializer) && /^(Map|Set|WeakMap|WeakSet)$/.test(initializer.expression.getText(sf))) {
            moduleState.push({ file, line: line(declaration), name: declaration.name.getText(sf), initializer: initializer.getText(sf).slice(0,200), mutableBinding: mutable })
          }
        }
      }
      const visit = (node: ts.Node): void => {
        if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument) && ts.isStringLiteral(node.argument.literal)) {
          edge(node, node.argument.literal.text, 'import-type', true, [node.qualifier?.getText(sf).split('.')[0] ?? '*'])
        }
        if (ts.isCallExpression(node)) {
          if (node.expression.kind === ts.SyntaxKind.ImportKeyword || ts.isIdentifier(node.expression) && node.expression.text === 'require') {
            const argument = node.arguments[0]
            if (argument && ts.isStringLiteralLike(argument)) edge(node, argument.text, node.expression.kind === ts.SyntaxKind.ImportKeyword ? 'lazy-import' : 'require', false, ['*'])
            else unresolvedDynamicImports.push({ file, line: line(node), expression: node.getText(sf) })
          }
          if (ts.isPropertyAccessExpression(node.expression) && ['glob','globEager'].includes(node.expression.name.text) && node.expression.expression.getText(sf) === 'import.meta') {
            const argument = node.arguments[0]
            const literals = argument && ts.isArrayLiteralExpression(argument) ? [...argument.elements] : argument ? [argument] : []
            if (!literals.length || literals.some(value => !ts.isStringLiteralLike(value))) {
              unresolvedDynamicImports.push({ file, line: line(node), expression: node.getText(sf) })
            } else for (const literal of literals) {
              if (!ts.isStringLiteralLike(literal)) continue
              const pattern = localPath(literal.text, file)
              if (!pattern || literal.text.startsWith('!')) { unresolvedDynamicImports.push({ file, line: line(node), expression: node.getText(sf) }); continue }
              const expression = globExpression(pattern)
              const matches = allFiles.filter(candidate => expression.test(candidate))
              const options = node.arguments[1]
              const eager = node.expression.name.text === 'globEager' || !!(options && ts.isObjectLiteralExpression(options) && options.properties.some(prop => ts.isPropertyAssignment(prop) && prop.name.getText(sf) === 'eager' && prop.initializer.kind === ts.SyntaxKind.TrueKeyword))
              for (const target of matches) edge(node, '@/' + target, eager ? 'glob-eager' : 'glob-lazy', false, ['*'])
              globImports.push({ file, line: line(node), pattern: literal.text, matched: matches.length })
            }
          }
        }
        ts.forEachChild(node, visit)
      }
      visit(sf)

      // <script setup> runs per component instance, not on module import. A
      // function/callback definition is not execution; follow only local calls
      // reached from module statements and immediately invoked functions. Imported
      // opaque factories need separate review; this does not execute production.
      if (!chunk.setup) {
        const functions = new Map<string, ts.ConciseBody>()
        for (const node of sf.statements) {
          if (ts.isFunctionDeclaration(node) && node.name && node.body) functions.set(node.name.text, node.body)
          if (ts.isVariableStatement(node)) for (const declaration of node.declarationList.declarations) {
            const value = declaration.initializer
            if (ts.isIdentifier(declaration.name) && value && (ts.isArrowFunction(value) || ts.isFunctionExpression(value))) functions.set(declaration.name.text, value.body)
          }
        }
        const eager = (node: ts.Node, trail: string[] = []): void => {
          if (ts.isFunctionLike(node)) return
          if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
            const callee = node.expression.getText(sf)
            let kind = ''
            if (/(^|\.)(setInterval|setTimeout|addEventListener|watch|watchEffect|subscribe)$/.test(callee) || /^(BroadcastChannel|WebSocket|EventSource)$/.test(callee)) kind = 'activation'
            if (/(localStorage|sessionStorage)(\?\.)?\.(getItem|setItem|removeItem|clear)$/.test(callee) || /(localStorage|sessionStorage)\?\.(getItem|setItem|removeItem|clear)$/.test(callee)) kind = 'storage'
            if (/^(fetch|(apiClient|axios|client|httpClient)\.(get|post|put|patch|delete|request))$/.test(callee)) kind = 'network'
            if (kind) eagerEffects.push({ file, line: line(node), callee, kind, trail, expression: node.getText(sf).slice(0,200) })
            if (ts.isIdentifier(node.expression) && functions.has(node.expression.text) && !trail.includes(node.expression.text)) {
              eager(functions.get(node.expression.text)!, [...trail, node.expression.text])
            }
            let expression: ts.Expression = node.expression
            while (ts.isParenthesizedExpression(expression)) expression = expression.expression
            if (ts.isArrowFunction(expression) || ts.isFunctionExpression(expression)) eager(expression.body, [...trail, 'IIFE'])
          }
          ts.forEachChild(node, child => { if (!ts.isFunctionLike(child)) eager(child, trail) })
        }
        for (const node of sf.statements) eager(node)
      }
    }
    modules.set(file, info)
  }

  // Type-only exceptions are contracts, not exemptions for runtime calls:
  // 1. Published feature-root type/capability/component projections (including
  //    Pick<typeof PublicFacet> and typeof a public presentation namespace).
  // 2. Named, exported API DTO declarations at their unique owning feature.
  // 3. The already canonical site announcement DTO module, reused verbatim.
  // No blanket permission for private model/store/command type dependencies.
  function typeContract(edge: Edge): string | null {
    if (!edge.typeOnly) return null
    if (publicEntry(edge.to)) return 'published-type-only-projection'
    if (edge.symbols.some(symbol => symbol === '*' || symbol === 'default')) return null
    const announcement = edge.to === 'features/site/model/announcement.ts'
    if (!announcement && !/^features\/[^/]+\/api\//.test(edge.to)) return null
    const target = modules.get(edge.to)
    return target && edge.symbols.every(symbol => target.exportedTypes.has(symbol)) ? announcement ? 'canonical-announcement-dto' : 'named-owner-api-dto' : null
  }
  const violations: Finding[] = [], typeContracts: TypeContract[] = []
  const fail = (rule: string, edge: Edge) => violations.push({ rule, ...edge })
  for (const edge of edges) {
    const targetRoot = edge.to.split('/')[0], sourceFeature = feature(edge.from), targetFeature = feature(edge.to)
    if (edge.from.startsWith('shared/') && (targetRoot === 'app' || targetRoot === 'features' || retiredRoots.has(targetRoot))) fail('SHARED_REVERSE_DEPENDENCY', edge)
    if (sourceFeature) {
      if (targetRoot === 'app') fail(edge.typeOnly ? 'FEATURE_APP_TYPE_DEPENDENCY' : 'FEATURE_APP_RUNTIME_DEPENDENCY', edge)
      if (targetFeature && targetFeature !== sourceFeature) {
        if (!edge.typeOnly) fail('CROSS_FEATURE_RUNTIME_DEPENDENCY', edge)
        else { const contract = typeContract(edge); if (contract) typeContracts.push({ ...edge, contract }); else fail('PRIVATE_CROSS_FEATURE_TYPE_DEPENDENCY', edge) }
      }
    }
    if (edge.from.startsWith('app/') && targetFeature && !publicEntry(edge.to)) {
      const contract = typeContract(edge)
      if (contract) typeContracts.push({ ...edge, contract })
      else fail(edge.typeOnly ? 'APP_PRIVATE_TYPE_DEPENDENCY' : 'APP_PRIVATE_RUNTIME_DEPENDENCY', edge)
    }
    if (!retiredRoots.has(edge.from.split('/')[0]) && retiredRoots.has(targetRoot)) fail(edge.typeOnly ? 'RETIRED_ROOT_TYPE_DEPENDENCY' : 'RETIRED_ROOT_RUNTIME_DEPENDENCY', edge)
  }
  const graph = new Map<string, Edge[]>()
  for (const edge of edges) if (!edge.typeOnly) { const list = graph.get(edge.from) ?? []; list.push(edge); graph.set(edge.from,list) }
  const queue = ['main.ts'], paths = new Map<string, Edge[]>([['main.ts', []]])
  for (let index = 0; index < queue.length; index++) for (const edge of graph.get(queue[index]) ?? []) {
    if (!paths.has(edge.to)) { paths.set(edge.to, [...paths.get(queue[index])!, edge]); queue.push(edge.to) }
  }
  const rootModules = files.filter(file => retiredRoots.has(file.split('/')[0])).map(file => ({
    file, runtimeReachable: paths.has(file), path: paths.get(file) ?? [],
    runtimeInbound: edges.filter(edge => edge.to === file && !edge.typeOnly), typeInbound: edges.filter(edge => edge.to === file && edge.typeOnly)
  }))
  const domainDTOs = [...modules.values()].flatMap(info => info.shapes).filter(shape => /^features\/[^/]+\/api\//.test(shape.file) || shape.file === 'features/site/model/announcement.ts')
  const sharedTypes = [...modules.values()].flatMap(info => info.shapes).filter(shape => shape.file.startsWith('shared/'))
  const sharedDTOCopies: ArchitectureAudit['sharedDTOCopies'] = []
  for (const shared of sharedTypes) if (!shared.generic) {
    const owner = domainDTOs.find(dto => !dto.generic && shared.signature === dto.signature && (shared.name === dto.name || shared.members >= 3))
    if (owner) sharedDTOCopies.push({ shared, owner })
  }
  // This is an intentional technical preference read, not a domain cache or
  // asynchronous activation. Nothing else in i18n is blanket-exempted.
  const technicalPreferenceReads = eagerEffects.filter(effect => effect.file === 'i18n/index.ts' && effect.kind === 'storage'
    && effect.callee === 'localStorage.getItem' && effect.trail.join('.') === 'getDefaultLocale' && effect.expression === 'localStorage.getItem(LOCALE_KEY)')
  return {
    sourceHashes, moduleCount: files.length, edgeCount: edges.length, violations, typeContracts,
    wildcardExports, unresolvedImports: edges.filter(edge => !allSet.has(edge.to)), unresolvedDynamicImports, globImports,
    moduleState, eagerEffects: eagerEffects.filter(effect => !technicalPreferenceReads.includes(effect)), technicalPreferenceReads,
    sharedDTOCopies, sharedTypes, rootModules
  }
}

// Production invariant assertions. The current production tree is the input;
// no snapshot of today's violations, .local manifest, or broad allowlist can
// make a real dependency violation pass. Route page imports are included.
const audit = auditProduction(resolve(dirname(fileURLToPath(import.meta.url)), '../..'))
const findings = (rules: string[]) => audit.violations.filter(item => rules.includes(item.rule))
  .map(item => item.rule + ' ' + item.from + ':' + item.line + ' -> ' + item.to
    + ' [' + item.symbols.join(', ') + ']' + (item.routePage ? ' (route/page; not exempt)' : ''))

describe('production architecture boundaries', () => {
  it('keeps shared independent of app, features and retired root buckets, including type edges', () => {
    expect(findings(['SHARED_REVERSE_DEPENDENCY'])).toEqual([])
  })
  it('keeps feature runtime dependencies inside their owner and shared technical modules', () => {
    expect(findings(['FEATURE_APP_RUNTIME_DEPENDENCY', 'CROSS_FEATURE_RUNTIME_DEPENDENCY'])).toEqual([])
  })
  it('limits cross-owner type dependencies to explicitly published projections and owner DTOs', () => {
    expect(findings(['FEATURE_APP_TYPE_DEPENDENCY', 'PRIVATE_CROSS_FEATURE_TYPE_DEPENDENCY', 'APP_PRIVATE_TYPE_DEPENDENCY'])).toEqual([])
  })
  it('makes app consume public feature entries, including lazy route components', () => {
    expect(findings(['APP_PRIVATE_RUNTIME_DEPENDENCY'])).toEqual([])
  })
  it('does not hide feature public surfaces behind wildcard exports', () => {
    expect(audit.wildcardExports.filter(item => publicEntry(item.file))).toEqual([])
  })
  it('keeps copied business DTO definitions out of shared', () => {
    expect(audit.sharedDTOCopies.map(item => item.shared.file + ':' + item.shared.line + ' ' + item.shared.name + ' duplicates ' + item.owner.file + '#' + item.owner.name)).toEqual([])
  })
  it('rejects statically visible timer, network, observer or nontechnical storage activation on module import', () => {
    expect(audit.eagerEffects).toEqual([])
  })
  it('resolves local dependency edges and statically bounds dynamic module loading', () => {
    expect(audit.unresolvedImports.map(item => item.from + ':' + item.line + ' -> ' + item.to)).toEqual([])
    expect(audit.unresolvedDynamicImports).toEqual([])
    expect(audit.globImports.filter(item => item.matched === 0)).toEqual([])
  })
  it('has no retired root facade execution path from the production entry', () => {
    expect(findings(['RETIRED_ROOT_RUNTIME_DEPENDENCY', 'RETIRED_ROOT_TYPE_DEPENDENCY'])).toEqual([])
    expect(audit.rootModules.filter(item => item.runtimeReachable).map(item => ({ file: item.file, path: item.path.map(edge => edge.from + ':' + edge.line + ' -> ' + edge.to) }))).toEqual([])
  })
})
