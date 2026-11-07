import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import type { Driver } from 'driver.js'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { useOnboardingTour } from '../useOnboardingTour'

const state = vi.hoisted(() => ({ driver: null as Driver | null }))
vi.mock('@/features/identity', () => ({
  useAuthStore: () => ({ user: { id: 7, role: 'user' }, isSuperAdmin: false, isSimpleMode: false })
}))
vi.mock('@/app/onboarding/store', () => ({
  useOnboardingStore: () => ({
    getDriverInstance: () => state.driver,
    setDriverInstance: (driver: Driver | null) => { state.driver = driver },
    isDriverActive: () => Boolean(state.driver?.isActive()),
    setControlMethods: vi.fn(),
    clearControlMethods: vi.fn()
  })
}))
vi.mock('@/app/onboarding/steps', () => ({
  getUserSteps: () => [{ popover: { title: 'Local guide fixture', description: 'Read-only tour fixture' } }],
  getAdminSteps: () => []
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  state.driver?.destroy()
  state.driver = null
  localStorage.removeItem('onboarding_tour_7_user_v4_interactive')
})

describe('onboarding close control', () => {
  it.each(['en', 'zh'] as const)('labels the actual driver close button in %s and keeps dismissal working', async locale => {
    const i18n = createI18n({ legacy: false, locale, messages: { en, zh } })
    const Harness = defineComponent({
      setup: () => useOnboardingTour({ autoStart: false }),
      template: '<div />'
    })
    const wrapper = mount(Harness, { global: { plugins: [i18n] } })
    await wrapper.vm.startTour()
    await flushPromises()
    const close = document.querySelector<HTMLButtonElement>('.driver-popover-close-btn')
    if (!close) throw new Error('Driver did not render its close button')
    expect(close.getAttribute('aria-label')).toBe({ en, zh }[locale].common.close)
    expect(document.querySelector('.driver-popover-progress-text')?.textContent).toBe('1 / 1')
    close.click()
    await flushPromises()
    expect(state.driver).toBeNull()
    expect(wrapper.vm.hasSeen()).toBe(true)
  })
})
