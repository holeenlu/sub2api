import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { installRoutePrefetch, useRoutePrefetch } from '../useRoutePrefetch'

const adjacency = [
  ['/admin/dashboard', ['/admin/accounts', '/admin/users']],
  ['/admin/accounts', ['/admin/dashboard', '/admin/users']],
  ['/admin/users', ['/admin/groups', '/admin/dashboard']],
  ['/admin/groups', ['/admin/subscriptions', '/admin/users']],
  ['/admin/subscriptions', ['/admin/groups']],
  ['/dashboard', ['/keys', '/usage']],
  ['/keys', ['/dashboard', '/usage']],
  ['/usage', ['/keys']],
  ['/profile', ['/dashboard', '/keys']]
] as const

function fixture() {
  const paths = [...new Set(adjacency.flatMap(([path, neighbors]) => [path, ...neighbors]))]
  const loads = new Map(paths.map(path => [path, vi.fn(async () => ({ render: () => null }))]))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [...loads].map(([path, load]) => ({ path, component: load }))
  })
  return { router, loads, prefetch: useRoutePrefetch(router) }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('requestIdleCallback', undefined)
  vi.stubGlobal('cancelIdleCallback', undefined)
})
afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('route prefetch lifecycle', () => {
  it.each(adjacency)('warms only the intended neighbors of %s', async (path, neighbors) => {
    const { loads, prefetch } = fixture()
    prefetch.triggerPrefetch({ path })
    expect([...loads.values()].every(load => load.mock.calls.length === 0)).toBe(true)
    await vi.advanceTimersByTimeAsync(1000)
    expect([...loads].filter(([, load]) => load.mock.calls.length).map(([name]) => name).sort()).toEqual([...neighbors].sort())
    expect(prefetch.prefetchedRoutes.value.has(path)).toBe(true)
    prefetch.dispose()
  })

  it('does not warm unknown routes or resolved component objects', async () => {
    const { loads, router, prefetch } = fixture()
    prefetch.triggerPrefetch({ path: '/unknown' })
    await vi.advanceTimersByTimeAsync(1000)
    expect([...loads.values()].every(load => !load.mock.calls.length)).toBe(true)
    router.addRoute({ path: '/keys', component: { render: () => null } })
    router.addRoute({ path: '/usage', component: { render: () => null } })
    const noLazy = useRoutePrefetch(createRouter({ history: createMemoryHistory(), routes: [
      { path: '/keys', component: { render: () => null } },
      { path: '/usage', component: { render: () => null } }
    ] }))
    noLazy.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(noLazy.prefetchedRoutes.value.size).toBe(0)
    noLazy.dispose()
    prefetch.dispose()
  })

  it('deduplicates completed warming and permits an explicit reset', async () => {
    const { loads, prefetch } = fixture()
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/keys')).toHaveBeenCalledTimes(1)
    prefetch.resetPrefetchState()
    expect(prefetch.prefetchedRoutes.value.size).toBe(0)
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/keys')).toHaveBeenCalledTimes(2)
    prefetch.dispose()
  })

  it('cancels an idle task when navigation changes or its owner cancels', async () => {
    const { loads, prefetch } = fixture()
    prefetch.triggerPrefetch({ path: '/admin/dashboard' })
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/admin/accounts')).not.toHaveBeenCalled()
    expect(loads.get('/keys')).toHaveBeenCalledOnce()
    prefetch.triggerPrefetch({ path: '/admin/users' })
    prefetch.cancelPendingPrefetch()
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/admin/groups')).not.toHaveBeenCalled()
    prefetch.dispose()
  })

  it.each(['reset', 'dispose'] as const)('rejects late import results after %s', async operation => {
    const { loads, prefetch } = fixture()
    let finish: (() => void) | undefined
    loads.get('/keys')?.mockImplementationOnce(() => new Promise(resolve => { finish = () => resolve({ render: () => null }) }))
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    if (operation === 'reset') prefetch.resetPrefetchState()
    else prefetch.dispose()
    finish?.()
    await vi.advanceTimersByTimeAsync(0)
    expect(prefetch.prefetchedRoutes.value.size).toBe(0)
    if (operation === 'dispose') {
      prefetch.triggerPrefetch({ path: '/profile' })
      await vi.advanceTimersByTimeAsync(1000)
      expect(loads.get('/dashboard')).not.toHaveBeenCalled()
    }
    prefetch.dispose()
  })

  it('keeps failed imports retryable without failing navigation', async () => {
    vi.spyOn(console, 'debug').mockImplementation(() => undefined)
    const { loads, prefetch } = fixture()
    loads.get('/keys')?.mockRejectedValueOnce(new Error('asset unavailable'))
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(prefetch.prefetchedRoutes.value.size).toBe(0)
    prefetch.triggerPrefetch({ path: '/dashboard' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(prefetch.prefetchedRoutes.value.has('/dashboard')).toBe(true)
    expect(loads.get('/keys')).toHaveBeenCalledTimes(2)
    prefetch.dispose()
  })

  it('uses the native idle deadline and cancels that same scheduler', () => {
    const request = vi.fn(() => 41)
    const cancel = vi.fn()
    vi.stubGlobal('requestIdleCallback', request)
    vi.stubGlobal('cancelIdleCallback', cancel)
    const { prefetch } = fixture()
    prefetch.triggerPrefetch({ path: '/dashboard' })
    expect(request).toHaveBeenCalledWith(expect.any(Function), { timeout: 2000 })
    prefetch.dispose()
    expect(cancel).toHaveBeenCalledWith(41)
  })

  it('unregisters the host hook and blocks idle work after teardown', async () => {
    const { router, loads } = fixture()
    const dispose = installRoutePrefetch(router)
    await router.push('/dashboard')
    dispose()
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/keys')).not.toHaveBeenCalled()
    await router.push('/profile')
    await vi.advanceTimersByTimeAsync(1000)
    expect(loads.get('/keys')).not.toHaveBeenCalled()
  })

  it('does not prefetch an aborted navigation', async () => {
    const { router, loads } = fixture()
    const dispose = installRoutePrefetch(router)
    router.beforeEach(() => false)
    await router.push('/dashboard')
    await vi.advanceTimersByTimeAsync(1000)
    expect([...loads.values()].every(load => !load.mock.calls.length)).toBe(true)
    dispose()
  })
})
