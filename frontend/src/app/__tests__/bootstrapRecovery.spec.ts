import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h, ref, type App, type EffectScope, type Ref } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'

interface FeatureLifetime {
  scope: EffectScope
  observed: number[]
  disposed: ReturnType<typeof vi.fn>
  storeDisposed: ReturnType<typeof vi.fn>
  storeObserved: number[]
}

const harness = vi.hoisted(() => ({
  apps: [] as App[],
  router: null as Router | null,
  featureSource: null as Ref<number> | null,
  features: [] as FeatureLifetime[],
  cleanupFailure: null as Error | null,
}))

// Observe real app instances; use/install/unmount and Vue Router are not replaced.
vi.mock('vue', async original => {
  const vue = await original<typeof import('vue')>()
  return {
    ...vue,
    createApp: (...args: Parameters<typeof vue.createApp>) => {
      const app = vue.createApp(...args)
      harness.apps.push(app)
      return app
    },
  }
})
vi.mock('../App.vue', async () => {
  const { h } = await import('vue')
  const { RouterView } = await import('vue-router')
  return { default: { render: () => h(RouterView) } }
})
vi.mock('@/app/router', () => ({
  get default() {
    if (!harness.router) throw new Error('Recovery router must be created before bootstrap')
    return harness.router
  },
}))
vi.mock('../http/install', () => ({ installApplicationHttp: vi.fn(() => vi.fn()) }))
vi.mock('@/i18n', () => ({ default: { install: vi.fn() }, initI18n: vi.fn(async () => undefined), configureLocaleMessageLoader: vi.fn() }))
vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({ siteName: 'Recovery test', initFromInjectedConfig: vi.fn() }),
}))
vi.mock('@/shared/lib/device', () => ({ isIOSDevice: () => false }))
vi.mock('../compositions/featureServices', async () => {
  const { effectScope, onScopeDispose, watch } = await import('vue')
  const { defineStore } = await import('pinia')
  return {
    installFeatureServices(app: App) {
      const source = harness.featureSource
      if (!source) throw new Error('Recovery scope source must be initialized')
      const lifetime: FeatureLifetime = { scope: effectScope(), observed: [], disposed: vi.fn(), storeDisposed: vi.fn(), storeObserved: [] }
      defineStore('bootstrap-owner-probe', () => {
        watch(source, value => lifetime.storeObserved.push(value), { immediate: true, flush: 'sync' })
        onScopeDispose(lifetime.storeDisposed)
        return {}
      })()
      lifetime.scope.run(() => {
        watch(source, value => lifetime.observed.push(value), { immediate: true, flush: 'sync' })
        onScopeDispose(lifetime.disposed)
      })
      harness.features.push(lifetime)
      const dispose = () => {
        lifetime.scope.stop()
        if (harness.cleanupFailure) throw harness.cleanupFailure
      }
      app.onUnmount(dispose)
      return dispose
    },
  }
})

import { bootstrap } from '../bootstrap'

const initialError = new Error('Initial lazy route import failed')
const loadPage = vi.fn()
const routeErrors = vi.fn()
const listenerReleases: Array<ReturnType<typeof vi.fn>> = []

beforeEach(() => {
  vi.clearAllMocks()
  harness.apps.length = 0
  harness.features.length = 0
  harness.featureSource = ref(0)
  harness.cleanupFailure = null
  listenerReleases.length = 0
  document.body.innerHTML = '<div id="app"></div>'

  const history = createMemoryHistory()
  const listen = history.listen.bind(history)
  vi.spyOn(history, 'listen').mockImplementation(callback => {
    // Delegate registration AND release to the real history. This only observes
    // the disposer Router installs; it does not model installedApps/ready/started.
    const release = vi.fn(listen(callback))
    listenerReleases.push(release)
    return release
  })
  loadPage.mockReset()
    .mockRejectedValueOnce(initialError)
    .mockResolvedValue({ render: () => h('p', { 'data-recovered-page': '' }, 'Recovered route') })
  harness.router = createRouter({ history, routes: [{ path: '/', component: loadPage }] })
  harness.router.onError(routeErrors)

  const warn = console.warn.bind(console)
  vi.spyOn(console, 'warn').mockImplementation((...args: unknown[]) => {
    const message = String(args[0])
    // Real Router logs its initial import rejection; Vue warns when uninstalling
    // an app before mount. Neither warning should hide another warning/error.
    if (message.includes('Unexpected error when starting the router:')
      || message.includes('Cannot unmount an app that is not mounted')) return
    warn(...args)
  })
})

afterEach(() => {
  for (const app of [...harness.apps].reverse()) app.unmount()
  // Ensure a deliberately red run cannot leave the fixture's scope alive.
  // Assertions always precede this fallback cleanup.
  for (const lifetime of harness.features) lifetime.scope.stop()
  harness.router?.options.history.destroy()
  harness.router = null
  harness.featureSource = null
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('bootstrap recovery with real Vue and Router lifecycles', () => {
  it.each([false, true])('preserves the original import error and releases router/scope with cleanup failure=%s', async cleanupFails => {
    const cleanupError = new Error('Feature cleanup failed after stopping its scope')
    harness.cleanupFailure = cleanupFails ? cleanupError : null
    const reportError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    await expect(bootstrap()).rejects.toBe(initialError)

    expect(routeErrors).toHaveBeenCalledWith(initialError, expect.any(Object), expect.any(Object))
    expect(loadPage).toHaveBeenCalledOnce()
    expect(listenerReleases).toHaveLength(1)
    expect(listenerReleases[0]).toHaveBeenCalledOnce()
    expect(document.querySelector('[data-recovered-page]')).toBeNull()

    const failed = harness.features[0]!
    expect(failed.scope.active).toBe(false)
    expect(failed.disposed).toHaveBeenCalledOnce()
    expect(failed.storeDisposed).toHaveBeenCalledOnce()
    harness.featureSource!.value = 1
    expect(failed.observed).toEqual([0])
    expect(failed.storeObserved).toEqual([0])
    if (cleanupFails) {
      expect(reportError).toHaveBeenCalledWith('Failed to release application initialization resources:', cleanupError)
    } else {
      expect(reportError).not.toHaveBeenCalled()
    }
  })

  it('restarts initial navigation on a second bootstrap without manual push and owns only the new scope', async () => {
    await expect(bootstrap()).rejects.toBe(initialError)
    const failed = harness.features[0]!
    harness.featureSource!.value = 1

    // The same real Router must restart itself through app.use(router). No push,
    // replace, isReady stub, timer advance or manual unmount repairs it here.
    await expect(bootstrap()).resolves.toBeUndefined()

    expect(loadPage).toHaveBeenCalledTimes(2)
    expect(routeErrors).toHaveBeenCalledOnce()
    expect(harness.router!.currentRoute.value.path).toBe('/')
    expect(document.querySelector('[data-recovered-page]')?.textContent).toBe('Recovered route')
    expect(listenerReleases).toHaveLength(2)
    expect(listenerReleases[0]).toHaveBeenCalledOnce()
    expect(listenerReleases[1]).not.toHaveBeenCalled()

    const recovered = harness.features[1]!
    expect(failed.scope.active).toBe(false)
    expect(failed.disposed).toHaveBeenCalledOnce()
    expect(recovered.scope.active).toBe(true)
    harness.featureSource!.value = 2
    expect(failed.observed).toEqual([0])
    expect(recovered.observed).toEqual([1, 2])

    harness.apps[1]!.unmount()
    expect(listenerReleases[1]).toHaveBeenCalledOnce()
    expect(recovered.scope.active).toBe(false)
    expect(recovered.disposed).toHaveBeenCalledOnce()
    harness.featureSource!.value = 3
    expect(recovered.observed).toEqual([1, 2])
  }, 3000)
})
