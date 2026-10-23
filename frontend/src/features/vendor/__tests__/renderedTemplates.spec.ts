// @vitest-environment node
import { expect, it } from 'vitest'
import { parse } from 'vue/compiler-sfc'

const sources = import.meta.glob<string>(
  [
    '/src/features/vendor/components/*.vue',
    '/src/views/vendor/*.vue',
    '/src/views/admin/{VendorsView,SettingsView}.vue',
  ],
  { eager: true, query: '?raw', import: 'default' },
)

it('compiles console wrappers as Vue fragments rather than inert native template elements', () => {
  const nativeTemplates: string[] = []
  type Node = {
    type: number
    tag?: string
    tagType?: number
    children?: Node[]
    loc: { start: { line: number } }
  }
  for (const [file, source] of Object.entries(sources)) {
    const { descriptor } = parse(source)
    const visit = (node: Node) => {
      if (node.type === 1 && node.tag === 'template' && node.tagType === 0)
        nativeTemplates.push(file + ':' + node.loc.start.line)
      node.children?.forEach(visit)
    }
    // The SFC's outer template is valid; native templates inside it are inert.
    descriptor.template?.ast?.children.forEach((node) => visit(node as Node))
  }
  expect(Object.keys(sources).length).toBeGreaterThan(20)
  expect(nativeTemplates).toEqual([])
})
