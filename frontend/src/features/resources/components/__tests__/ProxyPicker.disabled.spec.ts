import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import ProxyPicker from '../ProxyPicker.vue'
import type { Proxy } from '@/features/resources/api/proxyTypes'
import { managementProvide } from './managementTestServices'
import { editorTestServices } from '../../__tests__/editorTestServices'
const testProxy = vi.hoisted(() => vi.fn())
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/features/resources/proxies', () => ({ default: { testProxy } }))
enableAutoUnmount(afterEach)

describe('ProxyPicker disabled while open', () => {
  it('closes the real picker without changing selection and can reopen when reenabled', async () => {
    const proxy: Proxy = { id: 1, name: 'Proxy', host: 'localhost', port: 8080, protocol: 'http', username: null, password: null, status: 'active', created_at: '2026-01-01', updated_at: '2026-01-01' }
    const wrapper = mount(ProxyPicker, {
      props: { modelValue: null, proxies: [proxy] },
      global: { provide: managementProvide({}, editorTestServices()), stubs: { Transition: true } },
    })
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.find('.select-dropdown').exists()).toBe(true)
    await wrapper.setProps({ disabled: true })
    expect(wrapper.find('.select-dropdown').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(testProxy).not.toHaveBeenCalled()
    await wrapper.setProps({ disabled: false })
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.find('.select-dropdown').exists()).toBe(true)
  })
})
