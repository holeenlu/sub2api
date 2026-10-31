import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { formatDateTime } from '@/shared/lib/format'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

enableAutoUnmount(afterEach)
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

type BadgeProps = InstanceType<typeof PlatformTypeBadge>['$props']
const mountBadge = (props: Partial<BadgeProps> = {}) => mount(PlatformTypeBadge, {
  props: { platform: 'openai', type: 'oauth', ...props }
})

describe('PlatformTypeBadge recorded account metadata', () => {
  it('forwards attributes and listeners exactly once to the default badge', async () => {
    const click = vi.fn()
    const wrapper = mount(PlatformTypeBadge, {
      props: { platform: 'openai', type: 'oauth' },
      attrs: { id: 'recorded-badge', class: 'consumer-class', 'aria-label': 'Account metadata', onClick: click }
    })
    expect(wrapper.get('#recorded-badge').classes()).toContain('consumer-class')
    expect(wrapper.get('#recorded-badge').attributes('aria-label')).toBe('Account metadata')
    await wrapper.get('#recorded-badge').trigger('click')
    expect(click).toHaveBeenCalledTimes(1)
  })

  it('exposes metadata to an explicit slot without leaking fallback attributes or events', async () => {
    const click = vi.fn()
    const wrapper = mount(PlatformTypeBadge, {
      props: { platform: 'openai', type: 'oauth', planType: 'pro', authorizationModes: ['native'] },
      attrs: { class: 'badge-only', 'data-fallback': 'true', onClick: click },
      slots: { default: '<template #default="metadata"><p data-custom>{{ metadata.platformLabel }} / {{ metadata.typeLabel }} / {{ metadata.planLabel }} / {{ metadata.authorizationLabel }}</p></template>' }
    })
    expect(wrapper.text()).toBe('OpenAI / OAuth / Pro 200 / vendor.authorization.native')
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.find('[data-fallback]').exists()).toBe(false)
    expect(wrapper.get('[data-custom]').classes()).not.toContain('badge-only')
    await wrapper.get('[data-custom]').trigger('click')
    expect(click).not.toHaveBeenCalled()
  })

  it.each([
    ['setup-token-manual', 'Setup Token'],
    ['setup-token', 'OAuth Token'],
    ['refresh_token', 'Refresh Token'],
    ['oauth', 'OAuth'],
    ['future_method', 'OAuth']
  ])('displays credential method %s', (method, label) => {
    const wrapper = mountBadge({ method })
    expect(wrapper.text().replace(/\s+/g, '')).toBe(`OpenAI${label.replace(/\s+/g, '')}`)
  })

  it('preserves specific OpenAI auth modes before the generic method', async () => {
    const wrapper = mountBadge({ authMode: 'agentIdentity', method: 'refresh_token' })
    expect(wrapper.text()).toContain('Agent Identity')
    expect(wrapper.text()).not.toContain('Refresh Token')
    await wrapper.setProps({ authMode: 'personal_access_token' })
    expect(wrapper.text()).toContain('PAT')
    await wrapper.setProps({ authMode: undefined })
    expect(wrapper.text()).toContain('Refresh Token')
    await wrapper.setProps({ platform: 'anthropic', authMode: 'agentIdentity' })
    expect(wrapper.text()).not.toContain('Agent Identity')
  })

  it('keeps unknown account types readable without treating them as OAuth', () => {
    const wrapper = mountBadge({ type: 'future_type', authMode: 'agentIdentity' })
    expect(wrapper.text()).toContain('future_type')
    expect(wrapper.text()).not.toContain('Agent Identity')
  })

  it('shows billing metadata independently of a subscription plan', async () => {
    const wrapper = mountBadge({ platform: 'anthropic', billingType: 'apple_subscription' })
    expect(wrapper.text()).toContain('apple_subscription')
    await wrapper.setProps({ billingType: undefined })
    expect(wrapper.text()).not.toContain('apple_subscription')
  })

  it('renders only explicit authorization modes and responds to metadata removal', async () => {
    const wrapper = mountBadge()
    expect(wrapper.text()).not.toContain('vendor.authorization.')
    await wrapper.setProps({ authorizationModes: [] })
    expect(wrapper.text()).not.toContain('vendor.authorization.')
    await wrapper.setProps({ authorizationModes: ['native'] })
    expect(wrapper.text()).toContain('vendor.authorization.native')
    await wrapper.setProps({ authorizationModes: ['bps'] })
    expect(wrapper.text()).toContain('vendor.authorization.bps')
    await wrapper.setProps({ authorizationModes: ['bps', 'native'] })
    expect(wrapper.text()).toContain('vendor.authorization.dual')
    await wrapper.setProps({ authorizationModes: ['native', 'native'] })
    expect(wrapper.text()).toContain('vendor.authorization.native')
    expect(wrapper.text()).not.toContain('vendor.authorization.dual')
    await wrapper.setProps({ authorizationModes: undefined })
    expect(wrapper.text()).not.toContain('vendor.authorization.')
    await wrapper.setProps({ platform: 'anthropic', authorizationModes: ['native', 'bps'] })
    expect(wrapper.text()).not.toContain('vendor.authorization.')
  })

  it.each(['Max 5x', 'Max 20x', 'Pro'])('does not give Claude %s an OpenAI plan label', planType => {
    const wrapper = mountBadge({ platform: 'anthropic', planType })
    expect(wrapper.text()).toContain(planType)
    expect(wrapper.text()).not.toContain('Pro 200')
    expect(wrapper.text()).not.toContain('Pro 500')
  })
})

describe('PlatformTypeBadge compact status', () => {
  it.each([
    { mode: 'force_on', supported: false, label: 'compactSupported' },
    { mode: 'force_off', supported: true, label: 'compactUnsupported' },
    { mode: 'auto', supported: true, label: 'compactSupported' },
    { mode: 'auto', supported: false, label: 'compactUnsupported' },
    { mode: undefined, supported: undefined, label: 'compactAuto' },
    { mode: 'future_mode', supported: 'true', label: 'compactAuto' }
  ])('shows $label for mode=$mode, supported=$supported', ({ mode, supported, label }) => {
    const wrapper = mountBadge({ compact: { mode, supported } })
    expect(wrapper.text()).toContain(`admin.accounts.openai.${label}`)
    expect(wrapper.text()).not.toContain('vendor.authorization.')
  })

  it('renders one shared compact status for eligible account types only', async () => {
    const wrapper = mountBadge()
    expect(wrapper.text()).not.toContain('admin.accounts.openai.compact')
    await wrapper.setProps({ type: 'apikey', compact: { supported: false } })
    expect(wrapper.text()).toContain('admin.accounts.openai.compactUnsupported')
    await wrapper.setProps({ type: 'setup-token' })
    expect(wrapper.text()).not.toContain('admin.accounts.openai.compact')
    await wrapper.setProps({ type: 'oauth', platform: 'anthropic' })
    expect(wrapper.text()).not.toContain('admin.accounts.openai.compact')
  })

  it('formats valid checked-at metadata and omits invalid timestamps from the title', async () => {
    const checkedAt = '2026-10-26T08:00:00Z'
    const label = 'admin.accounts.openai.compactSupported'
    const wrapper = mountBadge({ compact: { supported: true, checkedAt } })
    expect(wrapper.get('div[title]').attributes('title')).toBe(
      `${label} | admin.accounts.openai.compactLastChecked: ${formatDateTime(new Date(checkedAt))}`
    )
    for (const invalid of ['', 'invalid-date', 42]) {
      await wrapper.setProps({ compact: { supported: true, checkedAt: invalid } })
      expect(wrapper.get('div[title]').attributes('title')).toBe(label)
    }
  })
})
