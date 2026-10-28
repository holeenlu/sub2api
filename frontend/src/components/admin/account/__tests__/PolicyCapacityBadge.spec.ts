import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PolicyCapacityBadge from '../PolicyCapacityBadge.vue'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'

enableAutoUnmount(afterEach)

const now = Date.parse('2026-10-28T00:00:00Z')
const hour = 60 * 60 * 1000
const deadline = (offset: number) => new Date(now + offset).toISOString()
const mountBadge = (until?: string | null) => mount(PolicyCapacityBadge, {
  props: { until },
  global: {
    plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { admin: zhAccounts } } })]
  }
})

describe('PolicyCapacityBadge', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the existing red label without a diagnostic action', async () => {
    const wrapper = mountBadge(deadline(hour))
    expect(wrapper.text()).toBe('疑似降智')
    expect(wrapper.get('span').classes()).toEqual(expect.arrayContaining([
      'bg-red-50', 'text-red-700', 'dark:bg-red-900/30', 'dark:text-red-300'
    ]))
    expect(wrapper.find('button').exists()).toBe(false)
    await wrapper.get('span').trigger('click')
    expect(wrapper.emitted('open')).toBeUndefined()
  })

  it.each([undefined, null, '', 'invalid-date', '2026-13-28T01:00:00Z', deadline(-1), deadline(0)])(
    'ignores a missing, invalid, or expired deadline: %s', (until) => {
      const wrapper = mountBadge(until)
      expect(wrapper.find('span').exists()).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
    }
  )

  it('disappears exactly at the server deadline without a prop refresh', async () => {
    const wrapper = mountBadge('2026-10-28T09:00:00+08:00')
    await vi.advanceTimersByTimeAsync(hour - 1)
    expect(wrapper.text()).toBe('疑似降智')
    await vi.advanceTimersByTimeAsync(1)
    expect(wrapper.find('span').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('reschedules repeated hits to the latest deadline', async () => {
    const wrapper = mountBadge(deadline(hour))
    await vi.advanceTimersByTimeAsync(hour / 2)
    await wrapper.setProps({ until: deadline(hour * 1.5) })
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(hour / 2)
    expect(wrapper.text()).toBe('疑似降智')
    await vi.advanceTimersByTimeAsync(hour / 2)
    expect(wrapper.find('span').exists()).toBe(false)

    await wrapper.setProps({ until: deadline(hour * 2.5) })
    expect(wrapper.text()).toBe('疑似降智')
    await wrapper.setProps({ until: deadline(hour * 2) })
    await vi.advanceTimersByTimeAsync(hour / 2)
    expect(wrapper.find('span').exists()).toBe(false)
  })

  it.each([null, 'invalid-date', deadline(-1)])('cancels the timer when updated to %s', async (until) => {
    const wrapper = mountBadge(deadline(hour))
    await wrapper.setProps({ until })
    expect(wrapper.find('span').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cleans up its outstanding timer on unmount', () => {
    const wrapper = mountBadge(deadline(hour))
    expect(vi.getTimerCount()).toBe(1)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
