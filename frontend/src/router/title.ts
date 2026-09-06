import { i18n } from '@/i18n'
import { BRAND_NAME } from '@/config/brand'
import type { RouteLocationNormalizedLoaded } from 'vue-router'
import type { CustomMenuItem } from '@/types'

/**
 * 统一生成页面标题，避免多处写入 document.title 产生覆盖冲突。
 * 优先使用 titleKey 通过 i18n 翻译，fallback 到静态 routeTitle。
 */
export function resolveDocumentTitle(
  routeTitle: unknown,
  siteName?: string,
  titleKey?: string,
  options?: { standalone?: boolean },
): string {
  const normalizedSiteName = typeof siteName === 'string' && siteName.trim() ? siteName.trim() : BRAND_NAME

  if (typeof titleKey === 'string' && titleKey.trim()) {
    const translated = i18n.global.t(titleKey)
    if (translated && translated !== titleKey) {
      // standalone：翻译本身已是完整标题（含站点名），不再附加站点名
      return options?.standalone ? translated : `${translated} - ${normalizedSiteName}`
    }
  }

  if (typeof routeTitle === 'string' && routeTitle.trim()) {
    return `${routeTitle.trim()} - ${normalizedSiteName}`
  }

  return normalizedSiteName
}

export function resolveRouteDocumentTitle(
  route: Pick<RouteLocationNormalizedLoaded, 'name' | 'params' | 'meta'>,
  siteName: string | undefined,
  customMenuItems: CustomMenuItem[] = [],
): string {
  const id = typeof route.params.id === 'string' ? route.params.id : ''
  const menuItem = route.name === 'CustomPage' && id
    ? customMenuItems.find((item) => item.id === id)
    : undefined
  const menuTitle = menuItem?.label.trim()

  return resolveDocumentTitle(
    menuTitle || route.meta.title,
    siteName,
    menuTitle ? undefined : (route.meta.titleKey as string),
    { standalone: !menuTitle && route.meta.titleStandalone === true },
  )
}

/**
 * 依 route.meta.metaDescriptionKey 更新 `<meta name="description">`。
 * 全站只保留一个 description 节点：有 key 就 upsert，没有就把上一页留下的移除，
 * 语言切换时重新调用即可跟着更新。
 */
export function applyRouteMetaDescription(
  route: Pick<RouteLocationNormalizedLoaded, 'meta'>,
): void {
  const key = route.meta.metaDescriptionKey
  const existing = document.head.querySelector<HTMLMetaElement>('meta[name="description"]')

  if (typeof key !== 'string' || !key.trim()) {
    existing?.remove()
    return
  }

  const content = i18n.global.t(key)
  if (!content || content === key) {
    existing?.remove()
    return
  }

  const node = existing ?? document.head.appendChild(document.createElement('meta'))
  node.setAttribute('name', 'description')
  node.setAttribute('content', content)
}
