import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(), isAuthenticated: false, isAdmin: false, isSimpleMode: false,
}))
const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API', backendModeEnabled: false, cachedPublicSettings: null as null | Record<string, unknown>,
}))

vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn(), isLoading: { value: false } }),
}))
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({ triggerPrefetch: vi.fn(), cancelPendingPrefetch: vi.fn(), resetPrefetchState: vi.fn() }),
}))

describe('API docs routes', () => {
  it('registers the docs pages as public routes', async () => {
    const { default: router } = await import('@/router')
    const overview = router.resolve('/docs')
    const model = router.resolve('/docs/models/gpt-6-astra')

    expect(overview.meta.requiresAuth).toBe(false)
    expect(overview.meta.titleKey).toBe('docs.pages.overview.title')
    expect(model.name).toBe('DocsModel')
    expect(model.meta.requiresAuth).toBe(false)
  })

  it('keeps the existing batch image guide route and its auth requirement', async () => {
    const { default: router } = await import('@/router')
    const batchGuide = router.resolve('/docs/batch-image')

    expect(batchGuide.name).toBe('BatchImageGuide')
    expect(batchGuide.meta.requiresAuth).toBe(true)
  })

  it('registers app guides and downloads as public documentation', async () => {
    const { default: router } = await import('@/router')
    const expected = [
      ['/apps', 'docs.pages.apps.title'],
      ['/apps/console', 'docs.pages.consoleGuide.title'],
      ['/apps/codex', 'docs.pages.codex.title'],
      ['/apps/claude-code', 'docs.pages.claudeCode.title'],
      ['/apps/claude-desktop', 'docs.pages.claudeDesktop.title'],
      ['/apps/session-recovery', 'docs.pages.sessionRecovery.title'],
      ['/apps/image-skills', 'docs.pages.imageSkills.title'],
      ['/apps/downloads', 'docs.pages.downloads.title'],
    ]

    for (const [path, titleKey] of expected) {
      const route = router.resolve(path)
      expect(route.meta.requiresAuth).toBe(false)
      expect(route.meta.titleKey).toBe(titleKey)
    }
    expect(router.resolve('/docs/apps').matched[0].redirect).toBe('/apps')
    expect(router.resolve('/docs/apps/codex').matched[0].redirect).toBe('/apps/codex')
    expect(router.resolve('/docs/downloads').matched[0].redirect).toBe('/apps/downloads')
  })
})
