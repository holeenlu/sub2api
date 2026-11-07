// @vitest-environment node
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { resolve } from 'node:path'
import { loadConfigFromFile } from 'vite'
import imageReferences from '@/shared/lib/__tests__/fixtures/imageReferences.json'

let transform: (html: string) => Promise<string>
let outDir: string | undefined

beforeAll(async () => {
  const loaded = await loadConfigFromFile({ command: 'serve', mode: 'development' }, resolve('vite.config.ts'))
  if (!loaded) throw new Error('Development config was not loaded')
  outDir = loaded.config.build?.outDir
  const plugins = (loaded.config.plugins ?? []).flat()
  const plugin = plugins.find(value => value && typeof value === 'object' && 'name' in value && value.name === 'inject-public-settings')
  if (!plugin || typeof plugin !== 'object' || !('transformIndexHtml' in plugin)) throw new Error('Public settings transform missing')
  const hook = plugin.transformIndexHtml
  if (!hook || typeof hook !== 'object' || !('handler' in hook) || typeof hook.handler !== 'function') throw new Error('Public settings transform handler missing')
  const handler = hook.handler
  transform = async html => {
    const result = await handler(html, { path: '/', filename: resolve('index.html') })
    if (typeof result !== 'string') throw new Error('Expected transformed HTML')
    return result
  }
})

afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs() })

describe('development branding boundary', () => {
  it.each(imageReferences)('uses the common image reference contract: $name', async ({ input, expected }) => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 0, data: { site_logo: input } }))))
    const result = await transform('<head><link rel="icon" href="/logo.svg" /></head>')
    const escaped = (expected || '/logo.svg').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)
    expect(result).toContain(`href="${escaped}"`)
  })

  it('keeps the default UI output in the frontend project', () => {
    expect(outDir).toBe('dist')
  })

  it.each(['true', 'false'])('uses the public same-origin favicon only for supplier build=%s', async supplier => {
    vi.stubEnv('VITE_VENDOR_PORTAL', supplier)
    const loaded = await loadConfigFromFile({ command: 'build', mode: 'production' }, resolve('vite.config.ts'))
    const plugin = loaded?.config.plugins?.flat().find(value => value && typeof value === 'object' && 'name' in value && value.name === 'vendor-favicon')
    if (!plugin || typeof plugin !== 'object' || !('transformIndexHtml' in plugin)) throw new Error('Supplier favicon transform missing')
    const hook = plugin.transformIndexHtml
    if (!hook || typeof hook !== 'object' || !('handler' in hook) || typeof hook.handler !== 'function') throw new Error('Supplier favicon handler missing')
    const result = await hook.handler('<head><link rel="icon" href="/logo.svg" /></head>', { path: '/', filename: resolve('index.html') })
    expect(result).toContain(supplier === 'true' ? 'href="/api/v1/settings/favicon"' : 'href="/logo.svg"')
  })

  it('preserves arbitrary configured text without letting it close the config script', async () => {
    const settings = { site_name: '</script><script>alert(1)</script>', site_logo: 'javascript:alert(2)', api_base_url: 'https://example.invalid/?q=<gateway>' }
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 0, data: settings }))))
    const result = await transform('<html><head><title>sub2api</title><link rel="icon" href="/logo.svg" /></head></html>')
    expect(result.match(/<script>/g)).toHaveLength(1)
    expect(result.match(/<\/script>/g)).toHaveLength(1)
    expect(result).toContain('&lt;/script&gt;')
    expect(result).toContain('href="/logo.svg"')
    const serialized = result.match(/window\.__APP_CONFIG__=(.*);<\/script>/)?.[1]
    expect(serialized).toBeDefined()
    expect(JSON.parse(serialized ?? '')).toEqual(settings)
  })

  it.each(['$&', "$'", '$`', '$$', '$1'])('keeps replacement token %s literal in configured values', async token => {
    const settings = { site_name: 'Demo ' + token, site_logo: 'https://example.invalid/' + token, api_base_url: 'https://gateway.invalid/' + token }
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 0, data: settings }))))
    const result = await transform('<html><head><title>sub2api</title><link rel="icon" href="/logo.svg" /></head><body><div id="app"></div></body></html>')
    const serialized = result.match(/window\.__APP_CONFIG__=(.*);<\/script>/)?.[1]
    expect(JSON.parse(serialized ?? '')).toEqual(settings)
    expect(result.match(/<body>/g)).toHaveLength(1)
    expect(result).toContain('Demo ' + token.replace(/&/g, '&amp;').replace(/'/g, '&#39;') + ' - AI API Gateway')
  })

  it('escapes a configured logo attribute while keeping its valid URL', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 0, data: { site_name: 'Configured & Site', site_logo: 'https://example.invalid/logo.png?x="&y=1' } }))))
    const result = await transform('<head><title>sub2api</title><link rel="icon" href="/logo.svg" /></head>')
    expect(result).toContain('<title>Configured &amp; Site - AI API Gateway</title>')
    expect(result).toContain('logo.png?x=&quot;&amp;y=1')
  })
})
