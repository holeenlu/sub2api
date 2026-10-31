import { readonly, ref } from 'vue'
import type { RouteLocationNormalized, Router } from 'vue-router'

type ComponentImportFn = () => Promise<unknown>
interface IdleTask { cancel(): void }

const PREFETCH_ADJACENCY: Record<string, string[]> = {
  // Admin routes - 预加载最常访问的相邻页面
  '/admin/dashboard': ['/admin/accounts', '/admin/users'],
  '/admin/accounts': ['/admin/dashboard', '/admin/users'],
  '/admin/users': ['/admin/groups', '/admin/dashboard'],
  '/admin/groups': ['/admin/subscriptions', '/admin/users'],
  '/admin/subscriptions': ['/admin/groups'],
  // User routes
  '/dashboard': ['/keys', '/usage'],
  '/keys': ['/dashboard', '/usage'],
  '/usage': ['/keys'],
  '/profile': ['/dashboard', '/keys']
}

function scheduleIdle(callback: () => void): IdleTask {
  if (typeof window.requestIdleCallback === 'function' && typeof window.cancelIdleCallback === 'function') {
    const handle = window.requestIdleCallback(callback, { timeout: 2000 })
    return { cancel: () => window.cancelIdleCallback(handle) }
  }
  const handle = setTimeout(callback, 1000)
  return { cancel: () => clearTimeout(handle) }
}

export interface RoutePrefetch {
  readonly prefetchedRoutes: Readonly<{ value: ReadonlySet<string> }>
  triggerPrefetch(route: Pick<RouteLocationNormalized, 'path'>): void
  cancelPendingPrefetch(): void
  resetPrefetchState(): void
  dispose(): void
}

/** Each host owns its idle callback and results; module imports cannot be aborted. */
export function useRoutePrefetch(router: Router): RoutePrefetch {
  const prefetchedRoutes = ref(new Set<string>())
  let pending: IdleTask | null = null
  let generation = 0
  let disposed = false

  const cancelPendingPrefetch = (): void => {
    generation++
    pending?.cancel()
    pending = null
  }

  const prefetchComponent = async (load: ComponentImportFn): Promise<boolean> => {
    try {
      await load()
      return true
    } catch (error) {
      // Failed asset warming must not break navigation or count as successful.
      if (import.meta.env.DEV) console.debug('[Prefetch] Failed to prefetch component:', error)
      return false
    }
  }

  const triggerPrefetch = (route: Pick<RouteLocationNormalized, 'path'>): void => {
    cancelPendingPrefetch()
    if (disposed || prefetchedRoutes.value.has(route.path)) return
    const adjacent = PREFETCH_ADJACENCY[route.path]
    if (!adjacent?.length) return
    const operation = generation
    const path = route.path
    pending = scheduleIdle(() => {
      pending = null
      if (disposed || generation !== operation) return
      const loaders: ComponentImportFn[] = []
      for (const target of adjacent) {
        const component = router.getRoutes().find(record => record.path === target)?.components?.default
        if (typeof component === 'function') loaders.push(component as ComponentImportFn)
      }
      if (!loaders.length) return
      void Promise.all(loaders.map(prefetchComponent)).then(results => {
        if (!disposed && generation === operation && results.every(Boolean)) {
          prefetchedRoutes.value.add(path)
        }
      })
    })
  }

  const resetPrefetchState = (): void => {
    cancelPendingPrefetch()
    prefetchedRoutes.value.clear()
  }
  const dispose = (): void => {
    disposed = true
    resetPrefetchState()
  }
  return { prefetchedRoutes: readonly(prefetchedRoutes), triggerPrefetch, cancelPendingPrefetch, resetPrefetchState, dispose }
}

/** Start warming only when the application host installs its navigation lifecycle. */
export function installRoutePrefetch(router: Router): () => void {
  const prefetch = useRoutePrefetch(router)
  const remove = router.afterEach((to, _from, failure) => {
    if (failure) prefetch.cancelPendingPrefetch()
    else prefetch.triggerPrefetch(to)
  })
  return () => {
    remove()
    prefetch.dispose()
  }
}
