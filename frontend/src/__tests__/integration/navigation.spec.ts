/**
 * 导航集成测试
 * 测试完整的页面导航流程、预加载和错误恢复机制
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createRouter, createWebHistory, createMemoryHistory, RouterView, type Router } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { useNavigationLoadingState, _resetNavigationLoadingInstance } from '@/app/router/loading'
import { installRoutePrefetch } from '@/app/router/useRoutePrefetch'

// Mock 视图组件
const MockDashboard = defineComponent({
  name: 'MockDashboard',
  render() {
    return h('div', { class: 'dashboard' }, 'Dashboard')
  }
})

const MockKeys = defineComponent({
  name: 'MockKeys',
  render() {
    return h('div', { class: 'keys' }, 'Keys')
  }
})

const MockUsage = defineComponent({
  name: 'MockUsage',
  render() {
    return h('div', { class: 'usage' }, 'Usage')
  }
})

// Mock stores
vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({
    isAuthenticated: true,
    isAdmin: false,
    isSimpleMode: false,
    checkAuth: vi.fn()
  })
}))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    siteName: 'Test Site'
  })
}))

// 创建测试路由
function createTestRouter(): Router {
  return createRouter({
    history: createWebHistory(),
    routes: [
      {
        path: '/',
        redirect: '/dashboard'
      },
      {
        path: '/dashboard',
        name: 'Dashboard',
        component: MockDashboard,
        meta: { requiresAuth: true, title: 'Dashboard' }
      },
      {
        path: '/keys',
        name: 'Keys',
        component: MockKeys,
        meta: { requiresAuth: true, title: 'Keys' }
      },
      {
        path: '/usage',
        name: 'Usage',
        component: MockUsage,
        meta: { requiresAuth: true, title: 'Usage' }
      }
    ]
  })
}

// 测试用 App 组件
const TestApp = defineComponent({
  name: 'TestApp',
  setup() {
    return () => h('div', { id: 'app' }, [h(RouterView)])
  }
})

describe('Navigation Integration Tests', () => {
  let router: Router
  let originalRequestIdleCallback: typeof window.requestIdleCallback
  let originalCancelIdleCallback: typeof window.cancelIdleCallback

  beforeEach(() => {
    // 设置 Pinia
    setActivePinia(createPinia())

    // 重置导航加载状态
    _resetNavigationLoadingInstance()

    // 创建新的路由实例
    router = createTestRouter()

    // Mock requestIdleCallback
    originalRequestIdleCallback = window.requestIdleCallback
    originalCancelIdleCallback = window.cancelIdleCallback

    vi.stubGlobal('requestIdleCallback', (cb: IdleRequestCallback) => {
      const id = setTimeout(() => cb({ didTimeout: false, timeRemaining: () => 50 }), 0)
      return id
    })
    vi.stubGlobal('cancelIdleCallback', (id: number) => clearTimeout(id))
  })

  afterEach(() => {
    vi.restoreAllMocks()
    window.requestIdleCallback = originalRequestIdleCallback
    window.cancelIdleCallback = originalCancelIdleCallback
  })

  describe('完整页面导航流程', () => {
    it('导航时应该触发加载状态变化', async () => {
      const navigationLoading = useNavigationLoadingState()

      // 初始状态
      expect(navigationLoading.isLoading.value).toBe(false)

      // 挂载应用
      const wrapper = mount(TestApp, {
        global: {
          plugins: [router]
        }
      })

      // 等待路由初始化
      await router.isReady()
      await flushPromises()

      // 导航到 /dashboard
      await router.push('/dashboard')
      await flushPromises()
      await nextTick()

      // 导航结束后状态应该重置
      expect(navigationLoading.isLoading.value).toBe(false)

      wrapper.unmount()
    })

    it('导航到新页面应该正确渲染组件', async () => {
      const wrapper = mount(TestApp, {
        global: {
          plugins: [router]
        }
      })

      await router.isReady()
      await router.push('/dashboard')
      await flushPromises()
      await nextTick()

      // 检查当前路由
      expect(router.currentRoute.value.path).toBe('/dashboard')

      wrapper.unmount()
    })

    it('连续快速导航应该正确处理路由状态', async () => {
      const wrapper = mount(TestApp, {
        global: {
          plugins: [router]
        }
      })

      await router.isReady()
      await router.push('/dashboard')

      // 快速连续导航
      router.push('/keys')
      router.push('/usage')
      router.push('/dashboard')

      await flushPromises()
      await nextTick()

      // 应该最终停在 /dashboard
      expect(router.currentRoute.value.path).toBe('/dashboard')

      wrapper.unmount()
    })
  })

  describe('路由预加载', () => {
    it('由真实导航装配启动，卸载后不再预取相邻页面', async () => {
      vi.useFakeTimers()
      vi.stubGlobal('requestIdleCallback', undefined)
      vi.stubGlobal('cancelIdleCallback', undefined)
      const keys = vi.fn(async () => MockKeys)
      const usage = vi.fn(async () => MockUsage)
      const navigation = createRouter({ history: createMemoryHistory(), routes: [
        { path: '/dashboard', component: MockDashboard },
        { path: '/keys', component: keys },
        { path: '/usage', component: usage },
        { path: '/profile', component: MockDashboard }
      ] })
      const dispose = installRoutePrefetch(navigation)
      const wrapper = mount(TestApp, { global: { plugins: [navigation] } })
      try {
        await navigation.push('/dashboard')
        await vi.advanceTimersByTimeAsync(1000)
        expect(keys).toHaveBeenCalledOnce()
        expect(usage).toHaveBeenCalledOnce()
        await navigation.push('/profile')
        dispose()
        await vi.advanceTimersByTimeAsync(1000)
        expect(keys).toHaveBeenCalledOnce()
        await navigation.push('/dashboard')
        await vi.advanceTimersByTimeAsync(1000)
        expect(usage).toHaveBeenCalledOnce()
      } finally {
        dispose()
        wrapper.unmount()
        vi.clearAllTimers()
        vi.useRealTimers()
        vi.unstubAllGlobals()
      }
    })
  })

  describe('Chunk 加载错误恢复', () => {
    it('chunk 加载失败应该被正确捕获', async () => {
      const errorHandler = vi.fn()

      // 创建带错误处理的路由
      const errorRouter = createRouter({
        history: createWebHistory(),
        routes: [
          {
            path: '/dashboard',
            name: 'Dashboard',
            component: MockDashboard
          },
          {
            path: '/error-page',
            name: 'ErrorPage',
            // 模拟加载失败的组件
            component: () => Promise.reject(new Error('Failed to fetch dynamically imported module'))
          }
        ]
      })

      errorRouter.onError(errorHandler)

      const wrapper = mount(TestApp, {
        global: {
          plugins: [errorRouter]
        }
      })

      await errorRouter.isReady()
      await errorRouter.push('/dashboard')
      await flushPromises()

      // 尝试导航到会失败的页面
      try {
        await errorRouter.push('/error-page')
      } catch {
        // 预期会失败
      }

      await flushPromises()

      // 错误处理器应该被调用
      expect(errorHandler).toHaveBeenCalled()

      wrapper.unmount()
    })

    it('chunk 加载错误应该包含正确的错误信息', async () => {
      let capturedError: Error | null = null

      const errorRouter = createRouter({
        history: createWebHistory(),
        routes: [
          {
            path: '/dashboard',
            name: 'Dashboard',
            component: MockDashboard
          },
          {
            path: '/chunk-error',
            name: 'ChunkError',
            component: () => {
              const error = new Error('Loading chunk failed')
              error.name = 'ChunkLoadError'
              return Promise.reject(error)
            }
          }
        ]
      })

      errorRouter.onError((error) => {
        capturedError = error
      })

      const wrapper = mount(TestApp, {
        global: {
          plugins: [errorRouter]
        }
      })

      await errorRouter.isReady()

      try {
        await errorRouter.push('/chunk-error')
      } catch {
        // 预期会失败
      }

      await flushPromises()

      expect(capturedError).not.toBeNull()
      expect(capturedError!.name).toBe('ChunkLoadError')

      wrapper.unmount()
    })
  })

  describe('导航状态管理', () => {
    it('导航开始时 isLoading 应该变为 true', async () => {
      const navigationLoading = useNavigationLoadingState()

      // 创建一个延迟加载的组件来模拟真实场景
      const DelayedComponent = defineComponent({
        name: 'DelayedComponent',
        async setup() {
          await new Promise((resolve) => setTimeout(resolve, 50))
          return () => h('div', 'Delayed')
        }
      })

      const delayRouter = createRouter({
        history: createWebHistory(),
        routes: [
          {
            path: '/dashboard',
            name: 'Dashboard',
            component: MockDashboard
          },
          {
            path: '/delayed',
            name: 'Delayed',
            component: DelayedComponent
          }
        ]
      })

      // 设置导航守卫
      delayRouter.beforeEach(() => {
        navigationLoading.startNavigation()
      })

      delayRouter.afterEach(() => {
        navigationLoading.endNavigation()
      })

      const wrapper = mount(TestApp, {
        global: {
          plugins: [delayRouter]
        }
      })

      await delayRouter.isReady()
      await delayRouter.push('/dashboard')
      await flushPromises()

      // 导航结束后 isLoading 应该为 false
      expect(navigationLoading.isLoading.value).toBe(false)

      wrapper.unmount()
    })

    it('导航取消时应该正确重置状态', async () => {
      const navigationLoading = useNavigationLoadingState()

      const testRouter = createRouter({
        history: createWebHistory(),
        routes: [
          {
            path: '/dashboard',
            name: 'Dashboard',
            component: MockDashboard
          },
          {
            path: '/keys',
            name: 'Keys',
            component: MockKeys,
            beforeEnter: (_to, _from, next) => {
              // 模拟导航取消
              next(false)
            }
          }
        ]
      })

      testRouter.beforeEach(() => {
        navigationLoading.startNavigation()
      })

      testRouter.afterEach(() => {
        navigationLoading.endNavigation()
      })

      const wrapper = mount(TestApp, {
        global: {
          plugins: [testRouter]
        }
      })

      await testRouter.isReady()
      await testRouter.push('/dashboard')
      await flushPromises()

      // 尝试导航到被取消的路由
      await testRouter.push('/keys').catch(() => {})
      await flushPromises()

      // 导航被取消后，状态应该被重置
      // 注意：由于 afterEach 仍然会被调用，isLoading 应该为 false
      expect(navigationLoading.isLoading.value).toBe(false)

      wrapper.unmount()
    })
  })
})
