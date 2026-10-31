import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import GroupCapacityBadge from '../GroupCapacityBadge.vue'
enableAutoUnmount(afterEach)
describe('GroupCapacityBadge', () => {
  it('preserves zero defaults and hides optional capacity dimensions', () => {
    const wrapper = mount(GroupCapacityBadge)
    expect(wrapper.findAll('svg')).toHaveLength(1)
    expect(wrapper.text().replace(/\s/g, '')).toBe('0/0')
    expect(wrapper.html()).toContain('bg-gray-100')
  })
  it('preserves saturation, usage and idle palette semantics for all dimensions', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 5, concurrencyMax: 5,
      sessionsUsed: 1, sessionsMax: 10, rpmUsed: 0, rpmMax: 60 } })
    expect(wrapper.findAll('svg')).toHaveLength(3)
    expect(wrapper.html()).toContain('bg-red-100')
    expect(wrapper.html()).toContain('bg-yellow-100')
    expect(wrapper.html()).toContain('bg-gray-100')
    expect(wrapper.text().replace(/\s/g, '')).toBe('5/51/100/60')
  })
  it('does not flag a zero limit as saturated', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 3, concurrencyMax: 0 } })
    expect(wrapper.html()).not.toContain('bg-red-100')
    expect(wrapper.html()).toContain('bg-yellow-100')
  })
})
