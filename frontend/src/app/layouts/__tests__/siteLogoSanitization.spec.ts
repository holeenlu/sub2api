import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { ref } from 'vue'
import { createBrandPorts, createLayoutFixture, layoutRouter } from './layoutTestFixture'
let state: ReturnType<typeof createLayoutFixture>
const keyQuery = vi.hoisted(() => vi.fn())
vi.mock('@/app/model/app', () => ({ useAppStore: () => state.app }))
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => state.auth }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => state.admin }))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => state.onboarding }))
vi.mock('@/features/jobs', () => ({ useBatchImageAccess: () => state.batchImage }))
vi.mock('@/features/usage/api/publicKeyUsage', () => ({ getPublicKeyUsage: keyQuery }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  state = createLayoutFixture()
  keyQuery.mockReset().mockRejectedValue(new Error('Unexpected private key query during layout rendering'))
})
afterEach(() => {
  expect(keyQuery).not.toHaveBeenCalled()
  document.documentElement.classList.remove('dark')
})

import AppSidebar from '../AppSidebar.vue'
import HomePage from '@/features/site/pages/HomePage.vue'
import PublicKeyUsagePage from '@/features/usage/pages/PublicKeyUsagePage.vue'

type Surface = 'sidebar' | 'home' | 'key-usage'
async function logo(surface: Surface, value: string) {
  state.app.siteLogo = value
  const ports = createBrandPorts(value)
  const router = await layoutRouter()
  const component = surface === 'sidebar' ? AppSidebar : surface === 'home' ? HomePage : PublicKeyUsagePage
  const wrapper = shallowMount(component, { global: { plugins: [router], provide: { ...state.provide, ...ports.provide }, stubs: { RouterLink: false } } })
  await flushPromises()
  return { wrapper, ports, image: () => wrapper.get(surface === 'home' ? '[data-testid="home-brand-logo"]' : 'img[alt="Logo"]') }
}

describe.each(['sidebar', 'home', 'key-usage'] as const)('%s logo boundary', surface => {
  it.each(['javascript:alert(1)', 'vbscript:msgbox(1)', 'data:text/html,<script>alert(1)</script>', '//evil.example/logo.svg', '<img src=x onerror=alert(1)>'])('falls back for unsafe logo input: %s', async value => {
    const result = await logo(surface, value)
    expect(result.image().attributes('src')).toBe('/logo.svg')
    expect(result.wrapper.find('script').exists()).toBe(false)
    expect(result.wrapper.find('img[onerror]').exists()).toBe(false)
  })
  it.each([
    ['https://cdn.example.test/brand/logo.png', 'https://cdn.example.test/brand/logo.png'],
    [' /uploads/brand/logo.svg ', '/uploads/brand/logo.svg'],
    ['data:image/png;base64,aGVsbG8=', 'data:image/png;base64,aGVsbG8='],
    ['data:image/jpeg;base64,aGVsbG8=', 'data:image/jpeg;base64,aGVsbG8='],
  ])('preserves supported absolute, relative or image-data logos: %s', async (value, expected) => {
    const result = await logo(surface, value)
    expect(result.image().element.tagName).toBe('IMG')
    expect(result.image().attributes('src')).toBe(expected)
  })
  it('keeps the default logo for empty settings', async () => {
    const result = await logo(surface, '')
    expect(result.image().attributes('src')).toBe('/logo.svg')
  })
  it('reactively rejects an unsafe replacement without keeping the old branding', async () => {
    const result = await logo(surface, '/uploads/valid.svg')
    expect(result.image().attributes('src')).toBe('/uploads/valid.svg')
    state.app.siteLogo = 'javascript:alert(1)'
    result.ports.current.value = { ...result.ports.current.value, site_logo: 'javascript:alert(1)' }
    await flushPromises()
    expect(result.image().attributes('src')).toBe('/logo.svg')
  })
})
