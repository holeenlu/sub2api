// @vitest-environment node
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'
import { describe, expect, it } from 'vitest'

describe('payment SDK production loading boundary', () => {
  it('keeps payment SDKs outside the static dependencies of a public entry', async () => {
    const root = fileURLToPath(new URL('../../..', import.meta.url))
    const entry = '\0payment-chunk-regression'
    // Use the real production chunk policy and dependencies. Source-level
    // dynamic imports alone cannot detect a manual chunk merging their effects.
    const result = await build({
      root,
      configFile: resolve(root, 'vite.config.ts'),
      logLevel: 'silent',
      plugins: [{
        name: 'payment-chunk-regression',
        resolveId: id => id === entry ? entry : null,
        load: id => id === entry ? `
          import axios from 'axios'
          export const http = axios
          export const airwallex = () => import('@airwallex/components-sdk')
          export const stripe = () => import('@stripe/stripe-js')
          export const spreadsheet = () => import('xlsx')
          export const qrCode = () => import('qrcode')
        ` : null,
      }],
      build: {
        write: false,
        emptyOutDir: false,
        minify: false,
        reportCompressedSize: false,
        rollupOptions: { input: entry, preserveEntrySignatures: 'strict' },
      },
    })
    if (Array.isArray(result) || !('output' in result)) {
      throw new Error('Expected one in-memory production bundle')
    }
    const chunks = result.output.filter(output => output.type === 'chunk')
    const publicEntry = chunks.find(chunk => chunk.isEntry)
    expect(publicEntry).toBeDefined()
    if (!publicEntry) return

    const visited = new Set<string>()
    const modules = new Set<string>()
    const visit = (file: string): void => {
      if (visited.has(file)) return
      visited.add(file)
      const chunk = chunks.find(candidate => candidate.fileName === file)
      if (!chunk) throw new Error(`Missing static chunk: ${file}`)
      for (const module of Object.keys(chunk.modules)) modules.add(module)
      for (const dependency of chunk.imports) visit(dependency)
    }
    visit(publicEntry.fileName)
    expect([...modules].some(module => module.includes('/axios/'))).toBe(true)
    expect([...modules].filter(module => /\/@(?:airwallex|stripe)\//.test(module))).toEqual([])
    expect([...modules].filter(module => /\/(?:xlsx|qrcode)\//.test(module))).toEqual([])
    // The SDKs must remain available to the checkout's dynamic imports.
    expect(publicEntry.dynamicImports).toHaveLength(4)
    for (const packageName of ['/@airwallex/components-sdk/', '/@stripe/stripe-js/', '/xlsx/', '/qrcode/']) {
      expect(chunks.some(chunk => !visited.has(chunk.fileName)
        && Object.keys(chunk.modules).some(module => module.includes(packageName)))).toBe(true)
    }
  }, 30_000)
})
