import { afterEach, describe, expect, it } from 'vitest'
import { DOMWrapper, enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { loadApplicationLocale } from '@/app/i18n'
import AccountInfoCell from '../AccountInfoCell.vue'
import { accountFixture } from '@/features/resources/__tests__/accountFixture'
import { createAccountDisplay } from '../../model/accountDisplay'

const messages = await loadApplicationLocale('en')
const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: messages } })
const message = (key: string) => i18n.global.t(key)
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

const identity = { name: 'openai-long-account-name-701ad4a5', id: 3, email: 'owner@example.invalid' }

type CellMetadata = Partial<InstanceType<typeof AccountInfoCell>['$props']>
function mountCell(metadata: CellMetadata = {}) {
  return mount(AccountInfoCell, {
    attachTo: document.body,
    props: { ...identity, platform: 'openai', type: 'oauth', ...metadata },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: messages } })] }
  })
}

function details() {
  const panel = document.querySelector('[role="tooltip"]')
  if (!(panel instanceof HTMLElement)) throw new Error('Account details are missing')
  return new DOMWrapper(panel)
}

describe('AccountInfoCell shared account details', () => {
  it('copies from the name immediately and provides a separate accessible pin control', async () => {
    const wrapper = mountCell()
    await wrapper.get('[data-account-copy]').trigger('click')
    expect(wrapper.emitted('copy')).toHaveLength(1)
    expect(wrapper.get('[data-account-details]').attributes('aria-expanded')).toBe('false')
    await wrapper.get('[data-account-details]').trigger('click')
    const panel = details()
    expect(panel.isVisible()).toBe(true)
    expect(wrapper.get('[data-account-details]').attributes('aria-controls')).toBe(panel.attributes('id'))
    expect(wrapper.get('[data-account-details]').attributes('aria-label')).toContain(identity.name)
    expect(wrapper.get('[data-account-copy]').attributes('aria-label')).toContain(identity.email)
    await panel.get('[data-account-info] button').trigger('click')
    expect(wrapper.emitted('copy')).toHaveLength(2)
    expect(wrapper.get('[data-account-details]').attributes('aria-expanded')).toBe('true')
    await wrapper.get('[data-account-details]').trigger('click')
    expect(panel.isVisible()).toBe(false)
  })

  it('renders readable metadata groups on a surface tooltip without proxy repetition', async () => {
    const wrapper = mountCell({
      planType: 'go', billingType: 'subscription', authorizationModes: ['native', 'bps'],
      subscriptionExpiresAt: '2099-11-19T12:00:00Z', privacyMode: 'training_set_failed',
      compact: { mode: 'auto', checkedAt: '2099-11-01T12:00:00Z' }
    })
    expect(wrapper.text()).not.toContain(message('vendor.direct'))
    expect(wrapper.text()).not.toContain(message('admin.accounts.privacyFailed'))
    await wrapper.get('[data-account-details]').trigger('click')
    const panel = details()
    expect(panel.classes()).toContain('bg-white')
    expect(panel.classes()).toContain('dark:bg-dark-800')
    expect(panel.findAll('dl')).toHaveLength(1)
    for (const value of [identity.name, identity.email, 'Go', message('admin.accounts.identitySummary.payment.subscription'), 'OpenAI · OAuth', '2099-11-19',
      message('vendor.authorization.dual'), message('admin.accounts.privacyFailed'), message('admin.accounts.openai.compactAuto').replace(/Compact\s*/, '').trim()]) {
      expect(panel.text()).toContain(value)
    }
    expect(panel.find('svg').exists()).toBe(false)
    expect(panel.text()).not.toContain(message('admin.accounts.columns.proxy'))
    expect(panel.text()).not.toContain('admin.accounts.')
    expect(panel.text()).not.toContain('vendor.')
    expect(panel.get('dd[title]').attributes('title')).toContain(message('admin.accounts.openai.compactLastChecked'))
  })

  const cases: { metadata: CellMetadata; expected: string[]; absent: string[] }[] = [
    { metadata: { platform: 'anthropic', planType: 'Max 20x', method: 'setup-token-manual', billingType: 'apple_subscription' }, expected: ['Max 20x', 'Setup Token', message('admin.accounts.identitySummary.payment.apple')], absent: ['Pro 200', 'Compact'] },
    { metadata: { platform: 'antigravity', planType: 'pro', privacyMode: 'privacy_set' }, expected: ['Pro', message('admin.accounts.privacyAntigravitySet')], absent: ['Pro 200', 'Compact'] },
    { metadata: { platform: 'grok', planType: 'supergrokheavy' }, expected: ['SuperGrok Heavy'], absent: ['Pro 200', 'Compact'] },
    { metadata: { type: 'apikey', planType: 'pro', subscriptionExpiresAt: '2099-11-19', privacyMode: 'training_off' }, expected: ['OpenAI · Key'], absent: ['Pro 200', '2099-11-19', message('admin.accounts.privacyTrainingOff')] },
    { metadata: { planType: 'future_plan', type: 'future_type' }, expected: ['future_plan', 'future_type'], absent: ['OAuth', 'Pro 200'] },
    { metadata: { planType: 'free', subscriptionExpiresAt: '2099-11-19' }, expected: ['Free'], absent: ['2099-11-19'] },
    { metadata: { planType: 'pro', subscriptionExpiresAt: 'invalid-date', privacyMode: 'future_privacy', compact: { supported: 'true' } }, expected: ['Pro 200', message('admin.accounts.openai.compactAuto').replace(/Compact\s*/, '').trim()], absent: ['invalid-date', 'Private', message('admin.accounts.openai.compactSupported')] }
  ]
  it.each(cases)('preserves platform metadata $metadata', async ({ metadata, expected, absent }) => {
    const wrapper = mountCell(metadata)
    await wrapper.get('[data-account-details]').trigger('click')
    for (const value of expected) expect(details().text()).toContain(value)
    for (const value of absent) expect(details().text()).not.toContain(value)
  })

  it('removes missing metadata without inventing plan, authorization or compact support', async () => {
    const wrapper = mountCell({ planType: 'pro', authorizationModes: ['native'], privacyMode: 'training_off', compact: { supported: true } })
    await wrapper.get('[data-account-details]').trigger('click')
    await wrapper.setProps({ email: null, planType: undefined, authorizationModes: undefined, privacyMode: undefined, compact: undefined })
    const panel = details()
    expect(panel.findAll('dl')).toHaveLength(1)
    expect(panel.get('[data-account-plan-detail]').text()).toBe(message('admin.accounts.identitySummary.unknown'))
    expect(panel.get('[data-account-payment-detail]').text()).toBe(message('admin.accounts.identitySummary.unknown'))
    expect(panel.get('[data-account-expiry-detail]').text()).toBe(message('admin.accounts.identitySummary.unknown'))
    expect(panel.get('[data-account-auth-detail]').text()).toBe('OAuth')
    expect(panel.text()).not.toContain('undefined')
    expect(panel.text()).not.toContain('null')
    expect(panel.text()).not.toContain(message('vendor.authorization.native'))
    expect(wrapper.get('[data-account-copy]').attributes('aria-label')).toContain(identity.name)
  })

  it('keeps exactly three summary items on the second row and separates credential and subscription dates', async () => {
    const wrapper = mountCell({ platform: 'anthropic', planType: 'Max 20x', rateLimitTier: 'default_claude_max_20x',
      billingType: 'stripe_subscription', credentialExpiresAt: '2099-11-19T12:30:00Z', subscriptionExpiresAt: '2099-12-31T00:00:00Z' })
    const row = wrapper.get('[data-account-metadata-row]')
    expect(row.element.children).toHaveLength(3)
    expect(row.get('[data-account-plan]').text()).toBe('Max 20x')
    expect(row.get('[data-account-payment]').text()).toBe(message('admin.accounts.identitySummary.payment.stripe'))
    expect(row.get('[data-account-credential-expiry] time').attributes('datetime')).toBe('2099-11-19T12:30:00.000Z')
    expect(row.text()).not.toContain('2099-12-31')
    await wrapper.get('[data-account-details]').trigger('click')
    const panel = details()
    expect(panel.get('[data-account-plan-detail]').text()).toBe('Max 20x · default_claude_max_20x')
    expect(panel.get('[data-account-expiry-detail] time').attributes('datetime')).toBe('2099-11-19T12:30:00.000Z')
    expect(panel.get('[data-account-auth-detail]').text()).toBe('OAuth')
    expect(panel.text()).toContain('2099-12-31')
    await wrapper.setProps({ credentialExpiresAt: undefined })
    expect(wrapper.find('[data-account-credential-expiry] time').exists()).toBe(false)
    expect(panel.get('[data-account-expiry-detail]').text()).toBe(message('admin.accounts.identitySummary.unknown'))
  })

  it('keeps unknown payment channels readable and never mistakes Claude plan tier for payment method', async () => {
    const wrapper = mountCell({ platform: 'anthropic', planType: 'Max 5x', subscriptionType: 'claude_max' })
    expect(wrapper.get('[data-account-payment]').text()).toBe(message('admin.accounts.identitySummary.unknown'))
    await wrapper.setProps({ billingType: 'future_payment_channel' })
    expect(wrapper.get('[data-account-payment]').text()).toBe('future_payment_channel')
    await wrapper.setProps({ billingType: undefined, subscriptionType: 'subscription' })
    expect(wrapper.get('[data-account-payment]').text()).toBe(message('admin.accounts.identitySummary.payment.subscription'))
  })

  it('preserves the validated homepage link and both application extension slots', async () => {
    const wrapper = mount(AccountInfoCell, {
      attachTo: document.body,
      props: { ...identity, platform: 'openai', type: 'oauth', homepage: 'https://account.example.invalid/profile' },
      slots: { details: '<p>Account detail extension</p>', default: '<p>Account status extension</p>' },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: messages } })] }
    })
    await wrapper.get('[data-account-details]').trigger('click')
    expect(details().get('a').attributes()).toMatchObject({ href: 'https://account.example.invalid/profile', target: '_blank', rel: 'noopener noreferrer' })
    expect(details().text()).toContain('Account detail extension')
    expect(wrapper.text()).toContain('Account status extension')
  })

  it.each([
    ['https://user:secret@account.example.invalid/v1?token=secret', 'https://account.example.invalid'],
    ['http://account.example.invalid/v1', 'http://account.example.invalid'],
    ['javascript:alert(1)', ''],
    ['data:text/html,unsafe', ''],
    ['/relative-path', '']
  ])('uses the main account URL boundary before rendering %s', async (baseUrl, expected) => {
    const { accountHomepageUrl } = createAccountDisplay(message)
    const account = accountFixture({ type: 'apikey', credentials: { base_url: baseUrl } })
    const wrapper = mountCell({ type: 'apikey' })
    await wrapper.setProps({ homepage: accountHomepageUrl(account) })
    await wrapper.get('[data-account-details]').trigger('click')
    const panel = details()
    if (expected) expect(panel.get('a').attributes('href')).toBe(expected)
    else expect(panel.find('a').exists()).toBe(false)
    expect(panel.html()).not.toContain('secret')
    expect(panel.html()).not.toContain('javascript:')
    expect(panel.html()).not.toContain('data:text')
  })
})
