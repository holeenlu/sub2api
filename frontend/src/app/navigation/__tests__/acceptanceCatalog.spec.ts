import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { parse } from 'vue/compiler-sfc'
import applicationRouter from '@/app/router'

interface CatalogCase {
  id: string
  roles: string[]
  route?: string
  initial_status: string
  fixture_ids: string[]
  preconditions: string[]
  steps: string[]
  expected: string[]
  source: string[]
  variants?: { panel?: string }[]
}
interface RouteInventory {
  pages: { path: string; routePattern: string; access: { allowedRoles: string[] }; subviews?: { id: string }[] }[]
  counts: { settingsSubviews: number }
  redirects: { path: string }[]
  aliases: { path: string; canonicalPath: string }[]
  fixture_catalog: Record<string, unknown>
}

// Repository definitions are checked against the assembled production Router;
// no mock route table or snapshot of current route counts can make this pass.
const inventory = JSON.parse(readFileSync(resolve('tests/acceptance/route-inventory.json'), 'utf8')) as RouteInventory
const catalog = JSON.parse(readFileSync(resolve('tests/acceptance/cases.json'), 'utf8')) as {
  cases: CatalogCase[]
  defaults: { cleanup: string[]; required_evidence: string[] }
}

describe('frontend acceptance catalog coverage', () => {
  it('keeps supplier baseline cases aligned with their actual positive role matrix', () => {
    for (const page of inventory.pages.filter(page => page.path.startsWith('/vendor/'))) {
      const expected = page.path === '/vendor/platform' ? ['super_admin']
        : page.path === '/vendor/profile' ? ['super_admin', 'vendor_level1', 'vendor_level2']
          : ['/vendor/children', '/vendor/child-accounts', '/vendor/child-proxies', '/vendor/child-usage', '/vendor/payables'].includes(page.path)
            ? ['vendor_level1'] : ['vendor_level1', 'vendor_level2']
      expect([...page.access.allowedRoles].sort(), page.path).toEqual([...expected].sort())
      for (const item of catalog.cases.filter(item => item.route === page.path && item.id.startsWith('FE-PAGE-') && /-(ENTRY|REFRESH|BACK)$/.test(item.id))) {
        expect([...item.roles].sort(), item.id).toEqual([...expected].sort())
      }
    }
  })
  it('covers every canonical rendered or redirect route without retired entries', () => {
    const actual = applicationRouter.getRoutes().filter(route => !route.aliasOf).map(route => route.path).sort()
    const documented = [...new Set([
      ...inventory.pages.map(page => page.routePattern),
      ...inventory.redirects.map(route => route.path)
    ])].sort()
    expect(documented).toEqual(actual)
  })

  it('records aliases separately and resolves their real canonical records', () => {
    const actual = applicationRouter.getRoutes().filter(route => route.aliasOf)
      .map(route => ({ path: route.path, canonicalPath: route.aliasOf?.path }))
      .sort((a, b) => a.path.localeCompare(b.path))
    expect(inventory.aliases.map(alias => ({ path: alias.path, canonicalPath: alias.canonicalPath }))
      .sort((a, b) => a.path.localeCompare(b.path))).toEqual(actual)
  })

  it('gives every page lifecycle cases and keeps case IDs, fixtures and source references valid', () => {
    expect(catalog.defaults.cleanup.length).toBeGreaterThan(0)
    expect(catalog.defaults.required_evidence.length).toBeGreaterThan(0)
    expect(new Set(catalog.cases.map(item => item.id)).size).toBe(catalog.cases.length)
    for (const page of inventory.pages) {
      for (const suffix of ['-ENTRY', '-REFRESH', '-BACK']) {
        expect(catalog.cases.filter(item => item.route === page.path && item.id.startsWith('FE-PAGE-') && item.id.endsWith(suffix)), page.path + suffix).toHaveLength(1)
      }
    }
    for (const item of catalog.cases) {
      expect(item.initial_status, item.id).toBe('not_run')
      expect(item.preconditions.length, item.id).toBeGreaterThan(0)
      expect(item.steps.length, item.id).toBeGreaterThan(0)
      expect(item.expected.length, item.id).toBeGreaterThan(0)
      for (const fixture of item.fixture_ids) expect(inventory.fixture_catalog, item.id).toHaveProperty(fixture)
      for (const source of item.source) expect(existsSync(resolve('..', source)), item.id + ': ' + source).toBe(true)
    }
  })

  it('covers current settings panels as well as top-level page routes', () => {
    const source = readFileSync(resolve('src/app/compositions/settings/SettingsWorkspace.vue'), 'utf8')
    const template = parse(source).descriptor.template?.content ?? ''
    const panels = new Set([...template.matchAll(/<([A-Z][A-Za-z0-9]+)(?:\s|\/?>)/g)]
      .map(match => match[1]).filter(name => name === 'AdminRolePermissions' || /(?:SettingsPanel|Settings|TemplateEditor|BackupPage)$/.test(name)))
    if (template.includes('services.DelegatedSettings')) panels.add('DelegatedSettings')
    for (const suffix of ['-FIELDS', '-WRITE']) {
      const covered = catalog.cases.filter(item => item.id.startsWith('FE-SETTING-') && item.id.endsWith(suffix))
        .map(item => item.variants?.[0]?.panel).sort()
      expect(covered).toEqual([...panels].sort())
    }
  })

  it('keeps the settings tab inventory aligned with the rendered workspace', () => {
    const source = readFileSync(resolve('src/app/compositions/settings/SettingsWorkspace.vue'), 'utf8')
    const script = parse(source).descriptor.scriptSetup?.content ?? ''
    const definition = /const tabs = \[([^\]]+)\] as const/.exec(script)?.[1]
    expect(definition).toBeDefined()
    const actual = [...(definition ?? '').matchAll(/'([^']+)'/g)].map(match => match[1])
    const documented = inventory.pages.find(page => page.path === '/admin/settings')?.subviews?.map(view => view.id)
    expect(documented).toEqual(actual)
    expect(inventory.counts.settingsSubviews).toBe(actual.length)
  })
})
