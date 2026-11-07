import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CloudGatewaySettings from '../CloudGatewaySettings.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const props = { accountId: 1, enabled: false, canEdit: true }

describe('Cloud native authorization switch', () => {
  it('provides only a switch without authorization, verification or usage reconciliation controls', async () => {
    const wrapper = mount(CloudGatewaySettings, { props })
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    wrapper.unmount()
  })

  it.each([true, false])('preserves editor permissions with Cloud=%s', async enabled => {
    const wrapper = mount(CloudGatewaySettings, { props: { ...props, enabled, canEdit: false } })
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    await wrapper.setProps({ canEdit: true })
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[!enabled]])
    wrapper.unmount()
  })
})
