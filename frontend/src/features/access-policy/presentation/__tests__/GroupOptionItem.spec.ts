import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import { groupTestContext } from './context'
const context = groupTestContext()
enableAutoUnmount(afterEach)

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        provide: context.provide,
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
  it('keeps custom zero rate, peak windows and selected checkmark', async () => {
    context.serverUtcOffset.value = '+05:30'
    const wrapper = mount(GroupOptionItem, { props: { name: 'Group', platform: 'openai', rateMultiplier: 2, userRateMultiplier: 0,
      peakRateEnabled: true, peakStart: '14:00', peakEnd: '18:00', peakRateMultiplier: 3, selected: true },
      global: { provide: context.provide, stubs: { GroupBadge: true } } })
    expect(wrapper.get('.line-through').text()).toBe('2x')
    expect(wrapper.text()).toContain('0x')
    expect(wrapper.text()).toContain('14:00-18:00 ×3 (UTC+05:30)')
    expect(wrapper.find('svg').exists()).toBe(true)
    await wrapper.setProps({ showCheckmark: false, peakRateEnabled: false })
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('UTC')
  })

  it('preserves native click fallthrough for the surrounding option control', async () => {
    const onClick = vi.fn()
    const wrapper = mount(GroupOptionItem, { props: { name: 'Group', platform: 'gemini' }, attrs: { onClick },
      global: { provide: context.provide, stubs: { GroupBadge: true } } })
    await wrapper.trigger('click')
    expect(onClick).toHaveBeenCalledOnce()
  })

})
