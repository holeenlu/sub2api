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

import AppHeader from '../AppHeader.vue'
import PublicKeyUsagePage from '@/features/usage/pages/PublicKeyUsagePage.vue'

async function docs(surface: 'header' | 'key-usage', value: string) {
  state.app.docUrl = value
  const ports = createBrandPorts('', value)
  const router = await layoutRouter()
  const wrapper = shallowMount(surface === 'header' ? AppHeader : PublicKeyUsagePage, {
    global: { plugins: [router], provide: { ...state.provide, ...ports.provide }, stubs: { RouterLink: false } },
  })
  await flushPromises()
  return { wrapper, ports, links: () => wrapper.findAll('a[target="_blank"]') }
}

describe.each(['header', 'key-usage'] as const)('%s documentation link boundary', surface => {
  it.each(['javascript:alert(1)', '  JaVaScRiPt:alert(1)', 'data:text/html,<script>alert(1)</script>', 'vbscript:msgbox(1)', '//evil.example/docs', '/relative-docs'])('does not render a navigable unsafe or unsupported URL: %s', async value => {
    const result = await docs(surface, value)
    expect(result.links()).toHaveLength(0)
    expect(result.wrapper.find('script').exists()).toBe(false)
  })
  it.each([
    [' https://docs.example.test/guide?lang=zh#start ', 'https://docs.example.test/guide?lang=zh#start'],
    ['HTTP://docs.example.test', 'http://docs.example.test/'],
  ])('renders normalized safe links with opener isolation: %s', async (value, expected) => {
    const result = await docs(surface, value)
    expect(result.links().length).toBeGreaterThan(0)
    for (const link of result.links()) {
      expect(link.attributes('href')).toBe(expected)
      expect(link.attributes('rel')?.split(/\s+/)).toEqual(expect.arrayContaining(['noopener', 'noreferrer']))
    }
  })
  it('removes a previously safe link when the live settings projection becomes unsafe', async () => {
    const result = await docs(surface, 'https://docs.example.test/')
    expect(result.links().length).toBeGreaterThan(0)
    state.app.docUrl = 'javascript:alert(1)'
    result.ports.current.value = { ...result.ports.current.value, doc_url: 'javascript:alert(1)' }
    await flushPromises()
    expect(result.links()).toHaveLength(0)
  })
})
