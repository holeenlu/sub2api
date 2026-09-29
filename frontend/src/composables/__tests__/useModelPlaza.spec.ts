import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, nextTick, reactive } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useModelPlaza } from '../useModelPlaza'

const mocks = vi.hoisted(() => ({ getModelPlaza: vi.fn(), auth: {} as { isAuthenticated: boolean; user: { id: number } | null } }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza: mocks.getModelPlaza }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
const consumer = defineComponent({ setup: () => useModelPlaza(), template: '<div>{{ data?.description }}</div>' })
beforeEach(() => { mocks.getModelPlaza.mockReset(); mocks.auth = reactive({ isAuthenticated: true, user: { id: 1 } }) })

describe('private plaza response lifecycle', () => {
  it('clears private data synchronously on logout and ignores old in-flight responses', async () => {
    let finishOld!: (v: unknown) => void
    mocks.getModelPlaza.mockResolvedValueOnce({ description: 'private user 1', groups: [] })
      .mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ description: 'anonymous', groups: [] })
    const wrapper = mount(consumer)
    await flushPromises()
    expect(wrapper.text()).toBe('private user 1')
    mocks.auth.user = { id: 2 }
    expect(wrapper.vm.data).toBeNull()
    const oldSignal = mocks.getModelPlaza.mock.calls[1][0].signal
    mocks.auth.isAuthenticated = false
    expect(oldSignal.aborted).toBe(true)
    await flushPromises()
    expect(wrapper.text()).toBe('anonymous')
    finishOld({ description: 'private user 2', groups: [] })
    await flushPromises()
    expect(wrapper.text()).toBe('anonymous')
    wrapper.unmount()
  })

  it.each([401, 404, 500])('preserves HTTP %s for a distinct error state', async status => {
    mocks.getModelPlaza.mockRejectedValue({ status })
    const wrapper = mount(consumer)
    await flushPromises()
    expect(wrapper.vm.errorStatus).toBe(status)
    expect(wrapper.vm.data).toBeNull()
    wrapper.unmount()
  })

  it('cancels on disposal and does not load an obsolete response', async () => {
    let finish!: (v: unknown) => void
    mocks.getModelPlaza.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(consumer)
    const vm = wrapper.vm
    const signal = mocks.getModelPlaza.mock.calls[0][0].signal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    finish({ description: 'late', groups: [] })
    await nextTick(); await flushPromises()
    expect(vm.data).toBeNull()
  })
})
