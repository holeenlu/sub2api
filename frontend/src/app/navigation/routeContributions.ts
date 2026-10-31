import type { RouteLocationNormalizedLoaded, RouteMeta, RouteRecordNormalized } from 'vue-router'
import type { RegisteredFeatureFlag } from './featureFlags'

/** FDOC-01: routes own stable navigation facts; app owns presentation/visibility.
 * Domain contributions only import vue-router types. This augmentation adds no
 * feature -> app runtime import, shared business DTO or second route registry.
 * Menu labels may intentionally differ from page titles; omitted labelKey uses
 * the existing page-title resolver (including the purchase billing-mode label).
 */
export type NavigationFeature = RegisteredFeatureFlag | 'batchImageAccess' | 'opsMonitoring' | 'adminPayment'
export type NavigationSurface = 'user' | 'personal' | 'admin'
export type NavigationIcon = 'dashboard' | 'key' | 'batchImage' | 'chart' | 'user' | 'users' | 'folder'
  | 'channel' | 'creditCard' | 'rechargeSubscription' | 'globe' | 'server' | 'plugin' | 'bell'
  | 'cog' | 'order' | 'orderList' | 'signal' | 'shield' | 'priceTag'

export interface RouteMenuContribution {
  readonly order: number
  readonly icon: NavigationIcon
  readonly labelKey?: string
  readonly group?: string
  readonly feature?: NavigationFeature
  readonly hideInSimpleMode?: boolean
  readonly personal?: boolean
  /** An existing user route can also appear in the simple admin section. */
  readonly adminSimpleOrder?: number
  readonly domId?: string
  readonly tour?: string
}

export interface RouteMenuGroup {
  readonly id: string
  readonly labelKey: string
  readonly icon: NavigationIcon
  readonly order: number
  readonly surface: 'admin' | 'user'
  readonly feature?: NavigationFeature
  readonly hideInSimpleMode?: boolean
}

declare module 'vue-router' {
  interface RouteMeta {
    /** Owner of the route contribution, including app-owned composition pages. */
    domain?: string
    navigation?: RouteMenuContribution
    /** Declared once on its owning route; children reference only this group id. */
    navigationGroup?: RouteMenuGroup
    /** Site-owned settings supply the instances of the existing custom route. */
    navigationSource?: 'custom'
  }
}

export interface NavigationRoute {
  readonly path: string
  readonly name: RouteRecordNormalized['name']
  readonly meta: Readonly<RouteMeta>
}

export interface NavigationContext {
  readonly simpleMode: boolean
  translate(key: string): string
  featureEnabled(feature: NavigationFeature): boolean | undefined
  canAccessAdminPage(path: string): boolean
  /** Reuses app/router/title; no duplicate purchase name or billing-label map. */
  titleKey(route: NavigationRoute): string | undefined
  /** Resolve matched parents through the live router, including parameters. */
  resolvePath?(route: NavigationRoute): string | undefined
}

export interface NavigationItem {
  readonly path: string
  readonly label: string
  readonly icon: NavigationIcon
  readonly domId?: string
  readonly tour?: string
  readonly children?: NavigationItem[]
  readonly expandOnly?: boolean
}

/** Read the already installed Router; never import route modules from Sidebar.
 * Aliases resolve normally but do not create duplicate menu/breadcrumb entries.
 * Components/loaders are deliberately absent from this projection.
 */
export function projectNavigationRoutes(records: readonly RouteRecordNormalized[]): NavigationRoute[] {
  return records.filter(record => !record.aliasOf).map(record => ({
    path: record.path,
    name: record.name,
    meta: { ...record.meta },
  }))
}

function routeLabel(route: NavigationRoute, context: NavigationContext, menu = false): string {
  const key = (menu ? route.meta.navigation?.labelKey : undefined) ?? context.titleKey(route)
  return key ? context.translate(key) : route.meta.title ?? ''
}

function visible(facts: Pick<RouteMenuContribution, 'feature' | 'hideInSimpleMode'>, context: NavigationContext): boolean {
  return !(context.simpleMode && facts.hideInSimpleMode)
    && (!facts.feature || context.featureEnabled(facts.feature) !== false)
}

export function buildNavigationMenu(
  routes: readonly NavigationRoute[], surface: NavigationSurface, context: NavigationContext,
): NavigationItem[] {
  const audience = surface === 'personal' ? 'user' : surface
  const leaves: Array<{ order: number; group?: string; item: NavigationItem }> = []
  for (const route of routes) {
    const menu = route.meta.navigation
    if (!menu) continue
    const simpleAdmin = surface === 'admin' && context.simpleMode && menu.adminSimpleOrder !== undefined
    if (!simpleAdmin && route.meta.shell !== audience) continue
    if (surface === 'personal' && menu.personal === false) continue
    if (!visible(menu, context)) continue
    // Preserve the existing admin-page permission policy, including user Keys
    // and site custom items being outside the admin path permission namespace.
    if (surface === 'admin' && route.path.startsWith('/admin') && !context.canAccessAdminPage(route.path)) continue
    leaves.push({
      order: simpleAdmin ? menu.adminSimpleOrder! : menu.order,
      group: menu.group,
      item: { path: route.path, label: routeLabel(route, context, true), icon: menu.icon, domId: menu.domId, tour: menu.tour },
    })
  }
  const roots = leaves.filter(leaf => !leaf.group)
  for (const route of routes) {
    const group = route.meta.navigationGroup
    if (!group || group.surface !== audience || !visible(group, context)) continue
    const children = leaves.filter(leaf => leaf.group === group.id).sort((a, b) => a.order - b.order).map(leaf => leaf.item)
    if (!children.length) continue
    roots.push({ order: group.order, item: {
      // A group is an expand-only key, not a second route or a synthetic URL.
      path: 'group:' + group.id, label: context.translate(group.labelKey), icon: group.icon,
      children, expandOnly: true,
    } })
  }
  return roots.sort((a, b) => a.order - b.order).map(root => root.item)
}

export interface NavigationBreadcrumb {
  readonly label: string
  readonly to?: string
}

/** Matched route titles and contributed groups are the same authorities as menu
 * projection; no hand-maintained path ancestry is introduced. Dynamic site
 * custom labels can be supplied by their existing settings owner.
 */
export function buildNavigationBreadcrumbs(
  route: Pick<RouteLocationNormalizedLoaded, 'matched' | 'path'>,
  routes: readonly NavigationRoute[],
  context: NavigationContext,
  customLabel?: string,
): NavigationBreadcrumb[] {
  const result: NavigationBreadcrumb[] = []
  const groups = new Set<string>()
  for (const matched of route.matched) {
    const source = matched.aliasOf ?? matched
    const contribution = routes.find(item => item.name === source.name && item.path === source.path)
    if (!contribution) continue
    const current = matched === route.matched[route.matched.length - 1]
    const menu = contribution.meta.navigation
    const permitted = !source.path.startsWith('/admin') || context.canAccessAdminPage(source.path)
    const menuVisible = !menu || visible(menu, context)
    const groupId = menu?.group
    const group = groupId && routes.find(item => item.meta.navigationGroup?.id === groupId)?.meta.navigationGroup
    const groupVisible = !group || visible(group, context)
    // Menu visibility is not route authorization: valid deep links still show
    // their current title, but hidden/denied ancestors must not become links.
    if (!current && (!permitted || !menuVisible || !groupVisible)) continue
    if (group && permitted && menuVisible && groupVisible && !groups.has(group.id)) {
      result.push({ label: context.translate(group.labelKey) })
      groups.add(group.id)
    }
    const label = contribution.meta.navigationSource === 'custom' && customLabel?.trim()
      ? customLabel.trim() : routeLabel(contribution, context)
    const parentPath = !current && !matched.redirect && matched.components
      ? context.resolvePath?.(contribution) ?? (!matched.path.includes(':') ? matched.path : undefined)
      : undefined
    if (label) result.push({ label, to: current ? route.path : parentPath })
  }
  return result
}
