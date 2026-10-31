import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import OpsErrorDetailsModal from '../OpsErrorDetailsModal.vue'

const api = vi.hoisted(() => ({ listRequestErrors: vi.fn(), listUpstreamErrors: vi.fn() }))
vi.mock('@/features/operations/api/ops', () => ({ opsAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/features/operations/services', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/operations/services')>()
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices()
  return { ...actual, useOperationsServices: () => services }
})

beforeEach(() => {
  vi.resetAllMocks()
  api.listRequestErrors.mockResolvedValue({ items: [], total: 0 })
  api.listUpstreamErrors.mockResolvedValue({ items: [], total: 0 })
})

async function openModal(errorType: 'request' | 'upstream' = 'request') {
  const wrapper = shallowMount(OpsErrorDetailsModal, {
    props: { show: false, timeRange: '1h', errorType },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('Ops error detail filter boundaries', () => {
  it.each(['request', 'upstream'] as const)('keeps %s endpoint, selected status and view semantics', async (errorType) => {
    const wrapper = await openModal(errorType)
    const request = errorType === 'upstream' ? api.listUpstreamErrors : api.listRequestErrors
    try {
      const selects = wrapper.findAllComponents({ name: 'Select' })
      expect(selects).toHaveLength(4)
      selects[0].vm.$emit('update:modelValue', 429)
      selects[3].vm.$emit('update:modelValue', 'excluded')
      await flushPromises()
      expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ status_codes: '429', view: 'excluded' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
      selects[0].vm.$emit('update:modelValue', 'other')
      await flushPromises()
      expect(request.mock.lastCall?.[0]).toMatchObject({ status_codes_other: '1' })
      expect(request.mock.lastCall?.[0]).not.toHaveProperty('status_codes')
      selects[0].vm.$emit('update:modelValue', null)
      await flushPromises()
      expect(request.mock.lastCall?.[0]).not.toHaveProperty('status_codes_other')
      expect(request.mock.lastCall?.[0]).not.toHaveProperty('status_codes')
      if (errorType === 'upstream') expect(api.listRequestErrors).not.toHaveBeenCalled()
      else expect(api.listUpstreamErrors).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('ignores invalid selection values without resetting valid filters or issuing another request', async () => {
    const wrapper = await openModal()
    try {
      const selects = wrapper.findAllComponents({ name: 'Select' })
      selects[0].vm.$emit('update:modelValue', 0)
      selects[3].vm.$emit('update:modelValue', 'all')
      await flushPromises()
      expect(api.listRequestErrors.mock.lastCall?.[0]).toMatchObject({ status_codes: '0', view: 'all' })
      const calls = api.listRequestErrors.mock.calls.length
      for (const value of [true, {}, '429', Number.NaN, Number.POSITIVE_INFINITY]) {
        selects[0].vm.$emit('update:modelValue', value)
        selects[3].vm.$emit('update:modelValue', value)
        await flushPromises()
      }
      expect(api.listRequestErrors).toHaveBeenCalledTimes(calls)
      expect(selects[0].props('modelValue')).toBe(0)
      expect(selects[3].props('modelValue')).toBe('all')
    } finally { wrapper.unmount() }
  })

  it('keeps custom time filters and the incomplete-range fallback', async () => {
    const wrapper = await openModal()
    try {
      await wrapper.setProps({ timeRange: 'custom', customStartTime: '2026-10-25T16:00:00Z', customEndTime: '2026-10-26T16:00:00Z' })
      await flushPromises()
      expect(api.listRequestErrors.mock.lastCall?.[0]).toMatchObject({ start_time: '2026-10-25T16:00:00Z', end_time: '2026-10-26T16:00:00Z' })
      expect(api.listRequestErrors.mock.lastCall?.[0]).not.toHaveProperty('time_range')
      await wrapper.setProps({ customStartTime: null })
      await flushPromises()
      expect(api.listRequestErrors.mock.lastCall?.[0]).toMatchObject({ time_range: '1h' })
      expect(api.listRequestErrors.mock.lastCall?.[0]).not.toHaveProperty('start_time')
      expect(api.listRequestErrors.mock.lastCall?.[0]).not.toHaveProperty('end_time')
    } finally { wrapper.unmount() }
  })
})
