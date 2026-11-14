import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import GroupBadge from '../GroupBadge.vue'
import type { GroupBadgeProps } from '../types'
import { groupTestContext } from './context'
enableAutoUnmount(afterEach)
function badge(props: GroupBadgeProps) {
  const context = groupTestContext()
  const i18n = createI18n({ legacy: false, locale: 'en', messages: {
    en: { groups: { subscription: 'Subscription' }, admin: { users: { expired: 'Expired', daysRemaining: '{days} days' } }, common: { peakRateTooltip: 'Peak: {window}' } },
    zh: { groups: { subscription: '订阅' }, admin: { users: { expired: '已过期', daysRemaining: '剩余 {days} 天' } }, common: { peakRateTooltip: '高峰：{window}' } },
  } })
  return { context, i18n, wrapper: mount(GroupBadge, { props, global: { provide: context.provide, plugins: [i18n] } }) }
}
describe('GroupBadge presentation', () => {
  it('uses reactive server UTC offset for peak windows, never browser timezone', async () => {
    const { wrapper, context } = badge({ name: 'Group', peakRateEnabled: true, peakStart: '14:00', peakEnd: '18:00', peakRateMultiplier: 0 })
    expect(wrapper.text()).toContain('14:00-18:00 ×0')
    expect(wrapper.text()).not.toContain('UTC')
    context.serverUtcOffset.value = '+08:00'
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[title]').attributes('title')).toBe('Peak: 14:00-18:00 ×0 (UTC+08:00)')
    context.serverUtcOffset.value = '-03:30'
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('(UTC-03:30)')
    context.serverUtcOffset.value = null
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).not.toContain('UTC')
    await wrapper.setProps({ showRate: false })
    expect(wrapper.text()).toBe('Group')
  })
  it('preserves custom zero rates and avoids strike-through for identical rates', async () => {
    const { wrapper } = badge({ name: 'Group', rateMultiplier: 2, userRateMultiplier: 0 })
    expect(wrapper.get('.line-through').text()).toBe('2x')
    expect(wrapper.text()).toContain('0x')
    await wrapper.setProps({ userRateMultiplier: 2 })
    expect(wrapper.find('.line-through').exists()).toBe(false)
    expect(wrapper.text()).toContain('2x')
  })
  it('keeps subscription labels reactive across locales and honors alwaysShowRate', async () => {
    const { wrapper, i18n } = badge({ name: 'Group', subscriptionType: 'subscription', rateMultiplier: 1.5, daysRemaining: 2 })
    expect(wrapper.text()).toContain('2 days')
    expect(wrapper.html()).toContain('bg-red-200/80')
    i18n.global.locale.value = 'zh'
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('剩余 2 天')
    await wrapper.setProps({ daysRemaining: 0 })
    expect(wrapper.text()).toContain('已过期')
    await wrapper.setProps({ daysRemaining: null })
    expect(wrapper.text()).toContain('订阅')
    await wrapper.setProps({ alwaysShowRate: true })
    expect(wrapper.text()).toContain('1.5x')
  })
  it.each([
    ['anthropic', 'bg-amber-50'], ['openai', 'bg-green-50'], ['gemini', 'bg-sky-50'],
    ['antigravity', 'bg-fuchsia-50'], ['grok', 'bg-zinc-100'], ['kimi', 'bg-pink-50'],
    ['zhipu', 'bg-indigo-50'], ['deepseek', 'bg-teal-50'], ['minimax', 'bg-rose-50'],
    ['composite', 'bg-cyan-50'], ['opencode_go', 'bg-emerald-100'],
  ] as const)('keeps the existing %s badge palette', (platform, color) => {
    const { wrapper } = badge({ name: 'Group', platform })
    expect(wrapper.classes()).toContain(color)
  })
  it('retains wrapping and incomplete peak-window behavior', () => {
    const { wrapper } = badge({ name: 'Long group name', truncate: false, peakRateEnabled: true, peakStart: '14:00' })
    expect(wrapper.html()).toContain('[overflow-wrap:anywhere]')
    expect(wrapper.find('[title]').exists()).toBe(false)
  })
})
