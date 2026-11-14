import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import ProxyPicker from '../ProxyPicker.vue'
import type { Proxy } from '@/features/resources/api/proxyTypes'
import { managementProvide } from './managementTestServices'
import { editorTestServices } from '../../__tests__/editorTestServices'

const testProxy = vi.hoisted(() => vi.fn())
vi.mock('@/features/resources/proxies', () => ({ default: { testProxy } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks() })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}
const proxy: Proxy = { id: 1, name: 'Selected proxy', protocol: 'http', host: '127.0.0.1', port: 8080, username: null, password: null, status: 'active', created_at: '2026-01-01', updated_at: '2026-01-01' }
async function openPicker(allowed: boolean | undefined = true) {
  const actor = ref('operator-a'), permission = ref(allowed)
  const services = editorTestServices()
  services.scopeKey = () => actor.value
  services.access = { ...services.access, get canTestProxies() { return permission.value } }
  const wrapper = mount(ProxyPicker, { props: { modelValue: null, proxies: [proxy] }, global: { provide: managementProvide({}, services), stubs: { Icon: true } } })
  await wrapper.get('.select-trigger').trigger('click')
  return { wrapper, actor, permission, services }
}

describe('resource proxy picker scope and authorization', () => {
  it.each([false, undefined])('keeps selection available without a diagnostic grant (%s)', async allowed => {
    const h = await openPicker(false)
    h.permission.value = allowed; await flushPromises()
    if (!h.wrapper.get('.select-trigger').classes().includes('select-trigger-open')) await h.wrapper.get('.select-trigger').trigger('click')
    expect(h.wrapper.find('.test-btn').exists()).toBe(false)
    expect(h.wrapper.find('.batch-test-btn').exists()).toBe(false)
    await h.wrapper.findAll('.select-option')[1].trigger('click')
    expect(h.wrapper.emitted('update:modelValue')).toEqual([[1]])
    expect(testProxy).not.toHaveBeenCalled()
  })

  it.each(['actor', 'revocation', 'disabled', 'candidates', 'close', 'escape', 'unmount'] as const)('drops a late test result on %s', async change => {
    const pending = deferred<{ success: boolean; message: string; country: string }>()
    testProxy.mockReturnValueOnce(pending.promise)
    const h = await openPicker()
    await h.wrapper.get('.test-btn').trigger('click')
    expect(testProxy).toHaveBeenCalledWith(1)
    if (change === 'actor') h.actor.value = 'operator-b'
    if (change === 'revocation') h.permission.value = false
    if (change === 'disabled') await h.wrapper.setProps({ disabled: true })
    if (change === 'candidates') await h.wrapper.setProps({ proxies: [{ ...proxy, host: '127.0.0.2' }] })
    if (change === 'close') await h.wrapper.get('.select-trigger').trigger('click')
    if (change === 'escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    if (change === 'unmount') h.wrapper.unmount()
    await flushPromises()
    pending.resolve({ success: true, message: 'ok', country: 'obsolete-result' }); await flushPromises()
    if (change !== 'unmount') {
      if (change === 'disabled') await h.wrapper.setProps({ disabled: false })
      if (!h.wrapper.get('.select-trigger').classes().includes('select-trigger-open')) await h.wrapper.get('.select-trigger').trigger('click')
      expect(h.wrapper.text()).not.toContain('obsolete-result')
    }
    expect(h.services.notices.showError).not.toHaveBeenCalled()
    expect(testProxy).toHaveBeenCalledTimes(1)
  })

  it('reports current failures without a silent retry and permits a deliberate retry', async () => {
    testProxy.mockRejectedValueOnce({ response: { data: { detail: 'Proxy route rejected' } } }).mockResolvedValueOnce({ success: true, message: 'ok', country: 'GB' })
    const h = await openPicker()
    await h.wrapper.get('.test-btn').trigger('click'); await flushPromises()
    expect(h.services.notices.showError).toHaveBeenCalledWith('Proxy route rejected')
    expect(h.wrapper.text()).toContain('admin.proxies.testFailed')
    expect(testProxy).toHaveBeenCalledTimes(1)
    await h.wrapper.get('.test-btn').trigger('click'); await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(2)
    expect(h.wrapper.text()).toContain('GB')
  })
})
