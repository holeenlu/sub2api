import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import DisplayModeSwitcher from '../components/DisplayModeSwitcher.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages

describe('supplier display preferences', () => {
  const listeners = new Set<(event: { matches: boolean }) => void>()
  let systemDark = false
  let wrapper: ReturnType<typeof mount> | undefined
  const mountMenu = () => mount(DisplayModeSwitcher, {
    attachTo: document.body,
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } })] },
  })
  const setSystem = async (dark: boolean) => {
    systemDark = dark
    listeners.forEach(listener => listener({ matches: dark }))
    await flushPromises()
  }
  beforeEach(() => {
    localStorage.removeItem('theme')
    document.documentElement.classList.remove('dark')
    systemDark = false
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      media: query,
      get matches() { return systemDark },
      onchange: null,
      addEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => listeners.add(listener as never),
      removeEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => listeners.delete(listener as never),
      addListener: (listener: (event: MediaQueryListEvent) => void) => listeners.add(listener as never),
      removeListener: (listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener as never),
      dispatchEvent: () => true,
    } as MediaQueryList))
  })
  afterEach(() => {
    wrapper?.unmount()
    listeners.clear()
    vi.restoreAllMocks()
    localStorage.removeItem('theme')
    document.documentElement.classList.remove('dark')
  })
  it('follows the OS only in system mode and retains an explicit selection on remount', async () => {
    wrapper = mountMenu()
    await flushPromises()
    await setSystem(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('[aria-checked="true"]').text()).toBe('System')
    await wrapper.findAll('[role="menuitemradio"]')[0].trigger('click')
    await flushPromises()
    expect(localStorage.getItem('theme')).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    await setSystem(false)
    await setSystem(true)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    wrapper.unmount()
    wrapper = mountMenu()
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('[aria-checked="true"]').text()).toBe('Light')
    await wrapper.findAll('[role="menuitemradio"]')[2].trigger('click')
    await flushPromises()
    expect(localStorage.getItem('theme')).toBe('auto')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    await setSystem(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
  it('moves focus with arrows and returns it to the trigger on Escape', async () => {
    wrapper = mountMenu()
    await wrapper.get('button').trigger('click')
    const first = wrapper.findAll('[role="menuitemradio"]')[0]
    await wrapper.get('[role="menu"]').trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(first.element)
    await first.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(wrapper.findAll('[role="menuitemradio"]')[1].element)
    await wrapper.get('[role="menu"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
