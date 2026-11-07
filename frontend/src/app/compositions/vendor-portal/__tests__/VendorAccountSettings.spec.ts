import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages
import VendorAccountSettings from '../components/VendorAccountSettings.vue'

const auth = vi.hoisted(() => ({
  user: {
    email: 'supplier@example.invalid',
    username: 'Supplier A',
    role: 'vendor_level2',
    created_at: '2026-10-12T20:22:00Z',
    avatar_url: null,
  },
}))
const app = vi.hoisted(() => ({
  cachedPublicSettings: { passkey_enabled: true },
}))
const vendor = vi.hoisted(() => ({
  vendor: { name: 'Supplier A', email: 'supplier@example.invalid' },
}))

vi.mock('@/features/identity', () => ({ useAuthStore: () => auth }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => app }))
vi.mock('@/features/supply/store', () => ({ useSupplyStore: () => vendor }))

const global = {
  plugins: [
    createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: nativeEn },
    }),
  ],
  stubs: {
    ProfileTotpCard: { template: '<div data-testid="totp-card" />' },
    ProfilePasskeyCard: {
      props: ['enabled'],
      template: '<div data-testid="passkey-card">{{ enabled }}</div>',
    },
    ProfilePasswordForm: {
      props: ['embedded'],
      template: '<div data-testid="password-form">{{ embedded }}</div>',
    },
  },
}

describe('VendorAccountSettings', () => {
  it('shows the supplier identity, role, registration time, and security cards', () => {
    const wrapper = mount(VendorAccountSettings, { global })

    expect(wrapper.get('[data-testid="vendor-account-settings"]').text()).toContain(
      'supplier@example.invalid',
    )
    expect(wrapper.text()).toContain('Tier-2 supplier')
    expect(wrapper.text()).toContain('Supplier A')
    expect(wrapper.text()).toContain('Status unavailable; manage on the password tab')
    expect(wrapper.get('[data-testid="totp-card"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="passkey-card"]').text()).toContain('true')
  })

  it('switches from account information to the embedded password flow', async () => {
    const wrapper = mount(VendorAccountSettings, { global })

    await wrapper.findAll('[role="tab"]')[1].trigger('click')

    expect(wrapper.get('[data-testid="password-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="totp-card"]').exists()).toBe(false)
    expect(wrapper.findAll('[role="tab"]')[0].attributes('aria-selected')).toBe('false')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
  })
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})

vi.mock('../panels', () => ({
  useVendorPortalSecurityPanels: () => ({
    password: { name: 'ProfilePasswordForm', props: ['embedded'], template: '<div data-testid="password-form">{{ embedded }}</div>' },
    totp: { name: 'ProfileTotpCard', template: '<div data-testid="totp-card" />' },
    passkey: { name: 'ProfilePasskeyCard', props: ['enabled'], template: '<div data-testid="passkey-card">{{ enabled }}</div>' },
  })
}))
