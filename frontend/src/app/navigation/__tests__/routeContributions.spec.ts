import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { parse } from 'vue/compiler-sfc'
import * as ts from 'typescript'
import applicationRouter from '@/app/router'
import { resolveRouteMetaKeys } from '@/app/router/title'
import { FeatureFlags, resolveFeatureFlag } from '../featureFlags'
import { buildNavigationBreadcrumbs, buildNavigationMenu, projectNavigationRoutes,
  type NavigationContext, type NavigationItem } from '../routeContributions'
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes'
import type { SiteBillingMode } from '@/features/pricing/settings'

const routes = projectNavigationRoutes(applicationRouter.getRoutes())
function context(options: { simple?: boolean; denied?: string[]; settings?: Partial<PublicSettings>; billing?: SiteBillingMode } = {}): NavigationContext {
  const settings: Partial<PublicSettings> = {
    channel_monitor_enabled: true, subscription_enabled: true, payment_enabled: true,
    available_channels_enabled: true, plugin_management_enabled: true, risk_control_enabled: true,
    ...options.settings,
  }
  return {
    simpleMode: options.simple ?? false,
    translate: key => key,
    canAccessAdminPage: path => !options.denied?.includes(path),
    featureEnabled: flag => flag === 'batchImageAccess' || flag === 'opsMonitoring' || flag === 'adminPayment'
      ? true : resolveFeatureFlag(settings, FeatureFlags[flag]),
    titleKey: route => resolveRouteMetaKeys(route, { billingMode: options.billing }).titleKey,
  }
}
function paths(items: NavigationItem[]): string[] {
  return items.flatMap(item => item.children ? paths(item.children) : [item.path])
}

describe('FDOC-01 route-owned navigation contributions', () => {
  it('declares every menu route once with an owner and every referenced group exactly once', () => {
    const menus = routes.filter(route => route.meta.navigation)
    expect(new Set(menus.map(route => route.path)).size).toBe(menus.length)
    expect(menus.every(route => typeof route.meta.domain === 'string' && ['admin', 'user'].includes(route.meta.shell ?? ''))).toBe(true)
    const groups = routes.flatMap(route => route.meta.navigationGroup ? [route.meta.navigationGroup.id] : [])
    expect(new Set(groups).size).toBe(groups.length)
    expect(groups.sort()).toEqual(['channels', 'orders', 'security-audit'])
    expect(menus.every(route => !route.meta.navigation?.group || groups.includes(route.meta.navigation.group))).toBe(true)
  })

  it('retains user and admin-personal order from the real assembled route metadata', () => {
    const expected = ['/dashboard', '/keys', '/batch-image', '/usage', '/available-channels', '/monitor', '/subscriptions', '/purchase', '/orders', '/profile']
    expect(paths(buildNavigationMenu(routes, 'user', context()))).toEqual(expected)
    expect(paths(buildNavigationMenu(routes, 'personal', context()))).toEqual(expected.slice(1))
  })

  it('publishes model management through its real lazy route and server page permissions', () => {
    const route = applicationRouter.getRoutes().find(record => record.path === '/admin/models')
    expect(route?.name).toBe('AdminModels')
    expect(route?.meta).toMatchObject({ domain: 'catalog', shell: 'admin', requiresAuth: true, requiresAdmin: true })
    expect(typeof route?.components?.default).toBe('function')
    const all = paths(buildNavigationMenu(routes, 'admin', context()))
    expect(all.filter(path => path === '/admin/models')).toHaveLength(1)
    expect(paths(buildNavigationMenu(routes, 'admin', context({ denied: ['/admin/models'] })))).not.toContain('/admin/models')
    expect(paths(buildNavigationMenu(routes, 'user', context()))).not.toContain('/admin/models')
  })

  it('retains the admin order and expands only the existing groups', () => {
    const menu = buildNavigationMenu(routes, 'admin', context())
    expect(paths(menu)).toEqual([
      '/admin/dashboard', '/admin/ops', '/admin/users', '/admin/groups', '/admin/models',
      '/admin/channels/pricing', '/admin/channels/monitor', '/admin/subscriptions',
      '/admin/accounts', '/admin/plugins', '/admin/announcements', '/admin/proxies',
      '/admin/risk-control', '/admin/prompt-audit', '/admin/orders/dashboard', '/admin/orders',
      '/admin/orders/plans', '/admin/usage', '/admin/audit-logs', '/admin/settings',
    ])
    expect(menu.filter(item => item.children).map(item => [item.label, item.expandOnly])).toEqual([
      ['nav.channelManagement', true], ['nav.securityAudit', true], ['nav.orderManagement', true],
    ])
  })

  it('keeps simple-mode Keys immediately before Settings without duplicating its route', () => {
    expect(paths(buildNavigationMenu(routes, 'admin', context({ simple: true })))).toEqual([
      '/admin/dashboard', '/admin/ops', '/admin/groups', '/admin/models', '/admin/accounts', '/admin/plugins',
      '/admin/announcements', '/admin/proxies', '/admin/risk-control', '/admin/prompt-audit',
      '/admin/usage', '/keys', '/admin/settings',
    ])
    expect(paths(buildNavigationMenu(routes, 'user', context({ simple: true })))).toEqual(['/dashboard', '/keys', '/monitor', '/profile'])
    expect(routes.filter(route => route.path === '/keys')).toHaveLength(1)
  })

  it('uses the existing opt-in/opt-out defaults and keeps menu hiding separate from routing permissions', () => {
    const options = context({ settings: {
      available_channels_enabled: undefined, plugin_management_enabled: undefined, risk_control_enabled: undefined,
      channel_monitor_enabled: undefined, subscription_enabled: undefined, payment_enabled: undefined,
    } })
    const user = paths(buildNavigationMenu(routes, 'user', options))
    expect(user).not.toContain('/available-channels')
    expect(user).toEqual(expect.arrayContaining(['/monitor', '/subscriptions', '/purchase', '/orders']))
    const admin = paths(buildNavigationMenu(routes, 'admin', options))
    expect(admin).not.toContain('/admin/plugins')
    expect(admin).not.toContain('/admin/risk-control')
    expect(admin).not.toContain('/admin/prompt-audit')
    expect(admin).toContain('/admin/channels/monitor')
    const disabled = context({ settings: { subscription_enabled: false, payment_enabled: false, channel_monitor_enabled: false } })
    expect(paths(buildNavigationMenu(routes, 'user', disabled))).not.toContain('/subscriptions')
    expect(paths(buildNavigationMenu(routes, 'admin', disabled))).not.toContain('/admin/subscriptions')
    expect(applicationRouter.resolve('/admin/subscriptions').meta.requiresSubscription).toBeUndefined()
    expect(applicationRouter.resolve('/subscriptions').meta.requiresSubscription).toBe(true)
  })

  it('filters individual permissions before dropping empty contributed groups', () => {
    const one = buildNavigationMenu(routes, 'admin', context({ denied: ['/admin/channels/pricing'] }))
    expect(one.find(item => item.label === 'nav.channelManagement')?.children?.map(item => item.path)).toEqual(['/admin/channels/monitor'])
    const none = buildNavigationMenu(routes, 'admin', context({ denied: ['/admin/channels/pricing', '/admin/channels/monitor', '/admin/users'] }))
    expect(none.some(item => item.label === 'nav.channelManagement')).toBe(false)
    expect(paths(none)).not.toContain('/admin/users')
    expect(paths(none)).toContain('/admin/accounts')
    expect(paths(buildNavigationMenu(routes, 'admin', { ...context({ simple: true }), canAccessAdminPage: () => false }))).toEqual(['/keys'])
  })

  it('keeps admin-only and batch-image facts in app filtering rather than route loaders', () => {
    const base = context()
    const disabled: NavigationContext = { ...base, featureEnabled: flag =>
      ['opsMonitoring', 'adminPayment', 'batchImageAccess'].includes(flag) ? false : base.featureEnabled(flag) }
    expect(paths(buildNavigationMenu(routes, 'admin', disabled))).not.toContain('/admin/ops')
    expect(buildNavigationMenu(routes, 'admin', disabled).some(item => item.label === 'nav.orderManagement')).toBe(false)
    expect(paths(buildNavigationMenu(routes, 'user', disabled))).not.toContain('/batch-image')
    expect(paths(buildNavigationMenu(routes, 'user', disabled))).toContain('/purchase')
  })

  it.each([
    ['recharge_only', 'nav.recharge'], ['subscription_only', 'nav.subscribe'], ['recharge_and_subscription', 'nav.buySubscription'],
  ] as const)('shares the %s purchase title with menu and breadcrumb', (billing, expected) => {
    const options = context({ billing })
    const menu = buildNavigationMenu(routes, 'user', options)
    const route = applicationRouter.resolve('/purchase')
    expect(menu.find(item => item.path === route.path)?.label).toBe(expected)
    expect(buildNavigationBreadcrumbs(route, routes, options)).toEqual([{ label: expected, to: '/purchase' }])
    expect(resolveRouteMetaKeys(route, { billingMode: billing }).titleKey).toBe(expected)
  })

  it('derives group ancestry and page labels without a breadcrumb path registry', () => {
    expect(buildNavigationBreadcrumbs(applicationRouter.resolve('/admin/channels/pricing'), routes, context())).toEqual([
      { label: 'nav.channelManagement' }, { label: 'admin.channels.title', to: '/admin/channels/pricing' },
    ])
    expect(buildNavigationBreadcrumbs(applicationRouter.resolve('/admin/orders/plans'), routes, context())).toEqual([
      { label: 'nav.orderManagement' }, { label: 'nav.paymentPlans', to: '/admin/orders/plans' },
    ])
  })

  it('keeps aliases, redirects, platform routes and site-owned dynamic titles intact', () => {
    expect(applicationRouter.resolve('/auth/oauth/callback').name).toBe('OAuthCallback')
    expect(applicationRouter.resolve('/docs/batch-image').name).toBe('BatchImageGuide')
    expect(applicationRouter.resolve('/admin/channels').matched[0]?.redirect).toBe('/admin/channels/pricing')
    expect(applicationRouter.resolve('/vendor/platform').name).toBe('VendorPortal')
    expect(applicationRouter.resolve('/vendor/platform').params.tab).toBe('platform')
    expect(applicationRouter.resolve('/vendor/security').matched[0]?.redirect).toBe('/vendor/profile')
    expect(routes.filter(route => route.name === 'BatchImageGuide')).toHaveLength(1)
    expect(buildNavigationBreadcrumbs(applicationRouter.resolve('/docs/batch-image'), routes, context())).toEqual([
      { label: 'batchImageGuide.title', to: '/docs/batch-image' },
    ])
    const docs = applicationRouter.resolve('/docs/models/example')
    expect(docs.meta.domain).toBe('site')
    expect(buildNavigationBreadcrumbs(docs, routes, context())).toEqual([{ label: 'docs.pages.models.title', to: '/docs/models/example' }])
    expect(buildNavigationBreadcrumbs(applicationRouter.resolve('/custom/status'), routes, context(), ' Status page ')).toEqual([
      { label: 'Status page', to: '/custom/status' },
    ])
  })

  it('projects changed metadata directly, strips loaders and never loads a page for navigation', () => {
    const load = vi.fn(async () => ({ render: () => null }))
    const router = createRouter({ history: createMemoryHistory(), routes: [{
      path: '/example', alias: '/old-example', name: 'Example', component: load,
      meta: { shell: 'user', domain: 'example', titleKey: 'example.title', navigation: { order: 1, icon: 'user' } },
    }] })
    const projected = projectNavigationRoutes(router.getRoutes())
    expect(projected).toHaveLength(1)
    expect(Object.keys(projected[0]!).sort()).toEqual(['meta', 'name', 'path'])
    expect(buildNavigationMenu(projected, 'user', context())[0]).toMatchObject({ path: '/example', label: 'example.title' })
    router.addRoute({ path: '/new-example', name: 'Example', component: load,
      meta: { shell: 'user', titleKey: 'example.newTitle', navigation: { order: 1, icon: 'user' } } })
    expect(buildNavigationMenu(projectNavigationRoutes(router.getRoutes()), 'user', context())[0]).toMatchObject({ path: '/new-example', label: 'example.newTitle' })
    expect(load).not.toHaveBeenCalled()
    router.options.history.destroy()
  })

  it('keeps Sidebar imports free of route assemblies and its own stable URL literals', () => {
    const source = readFileSync(resolve('src/app/layouts/AppSidebar.vue'), 'utf8')
    const script = parse(source).descriptor.scriptSetup!.content
    const tree = ts.createSourceFile('AppSidebar.ts', script, ts.ScriptTarget.Latest, true)
    const runtimeImports = tree.statements.filter(ts.isImportDeclaration).filter(node => !node.importClause?.isTypeOnly)
      .map(node => ts.isStringLiteral(node.moduleSpecifier) ? node.moduleSpecifier.text : '')
    expect(runtimeImports).not.toContain('@/app/router')
    expect(runtimeImports.some(name => /\/routes$/.test(name))).toBe(false)
    const stablePaths: string[] = []
    function visit(node: ts.Node) {
      if (ts.isStringLiteral(node) && /^\/(admin|keys|dashboard|usage|purchase|orders|subscriptions|profile|monitor|available-channels|batch-image|custom)(\/|$)/.test(node.text)) stablePaths.push(node.text)
      ts.forEachChild(node, visit)
    }
    visit(tree)
    expect(stablePaths).toEqual([])
  })
})
