import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { AlertRule, opsAPI } from '@/features/operations/api/ops'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

import OpsAlertRulesCard from '../OpsAlertRulesCard.vue'
import { ref } from 'vue'
const { createAlertRule, updateAlertRule, listAlertRules } = vi.hoisted(() => ({
  createAlertRule: vi.fn(),
  updateAlertRule: vi.fn<typeof opsAPI.updateAlertRule>(),
  listAlertRules: vi.fn<typeof opsAPI.listAlertRules>().mockResolvedValue([])
}))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(true) }))

vi.mock('@/features/operations/api/ops', () => ({ opsAPI: { listAlertRules, createAlertRule, updateAlertRule } }))

async function openRule() {
  const wrapper = shallowMount(OpsAlertRulesCard, {
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } },
  })
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text().includes('admin.ops.alertRules.create'))!.trigger('click')
  await wrapper.find('input[type="text"]').setValue('Error rate')
  return wrapper
}
describe('alert rule integer durations', () => {
  beforeEach(() => { vi.clearAllMocks() })
  it.each(['1.5', '0.5'])('rejects fractional duration %s before submitting', async value => {
    const wrapper = await openRule()
    const input = wrapper.find(value === '1.5' ? 'input[min="1"][max="1440"]' : 'input[min="0"][max="1440"]')
    await input.setValue(value)
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    expect(wrapper.text()).toContain(value === '1.5' ? 'admin.ops.alertRules.validation.sustainedRange' : 'admin.ops.alertRules.validation.cooldownRange')
    await save.trigger('click')
    expect(createAlertRule).not.toHaveBeenCalled()
  })
  it('allows whole-minute boundaries and a fractional metric threshold', async () => {
    const wrapper = await openRule()
    await wrapper.get('input[min="1"][max="1440"]').setValue('1440')
    await wrapper.get('input[min="0"][max="1440"]').setValue('0')
    await wrapper.get('input[type="number"]:not([min])').setValue('1.25')
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()
    expect(createAlertRule).toHaveBeenCalledWith(expect.objectContaining({ sustained_minutes: 1440, cooldown_minutes: 0, threshold: 1.25 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })
})

describe('alert rule edit projection', () => {
  function rule(): AlertRule {
    return {
      id: 42, name: 'Existing rule', description: undefined, enabled: false,
      metric_type: 'error_rate', operator: '>', threshold: 0, window_minutes: 5,
      sustained_minutes: 1, severity: 'warning', cooldown_minutes: 0, notify_email: false,
      filters: { group_id: 7, optional: undefined, nested: { empty: null, enabled: false, count: 0, values: [undefined, null, false, 0, { value: 1 }] } },
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-02T00:00:00Z', last_triggered_at: null
    }
  }

  async function openEdit(source: AlertRule) {
    listAlertRules.mockResolvedValue([source])
    const wrapper = shallowMount(OpsAlertRulesCard, {
      global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } }
    })
    await flushPromises()
    const edit = wrapper.findAll('button').find(button => button.text() === 'common.edit')
    if (!edit) throw new Error('Edit button missing')
    await edit.trigger('click')
    return wrapper
  }

  beforeEach(() => { vi.clearAllMocks() })
  afterEach(() => { listAlertRules.mockResolvedValue([]) })

  it('saves editable fields exactly and independently copies every nested filter container', async () => {
    const source = rule()
    updateAlertRule.mockResolvedValue(source)
    const wrapper = await openEdit(source)
    await wrapper.get('input[type="text"]').setValue('Edited rule')
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    if (!save) throw new Error('Save button missing')
    await save.trigger('click')
    await flushPromises()
    expect(updateAlertRule).toHaveBeenCalledWith(42, {
      name: 'Edited rule', description: undefined, enabled: false, metric_type: 'error_rate', operator: '>', threshold: 0,
      window_minutes: 5, sustained_minutes: 1, severity: 'warning', cooldown_minutes: 0, notify_email: false, filters: source.filters
    }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const payload = updateAlertRule.mock.calls[0]?.[1]
    expect(payload).toHaveProperty('description', undefined)
    expect(payload).not.toHaveProperty('id')
    expect(payload).not.toHaveProperty('created_at')
    expect(payload).not.toHaveProperty('last_triggered_at')
    expect(payload?.filters).not.toBe(source.filters)
    expect(payload?.filters).toStrictEqual(source.filters)
    expect(payload?.filters).toHaveProperty('optional', undefined)
    const nested = payload?.filters?.nested
    if (typeof nested !== 'object' || nested === null || !('values' in nested) || !Array.isArray(nested.values)) throw new Error('Nested filter missing')
    expect(nested).not.toBe(source.filters?.nested)
    nested.values.push('draft-only')
    const item: unknown = nested.values[4]
    if (typeof item !== 'object' || item === null || !('value' in item)) throw new Error('Nested item missing')
    item.value = 9
    expect(source).toStrictEqual(rule())
  })

  it('cancelled group removal and text edits leave the server snapshot intact when reopened', async () => {
    const source = rule()
    const wrapper = await openEdit(source)
    await wrapper.get('input[type="text"]').setValue('Unsaved')
    const groupSelect = wrapper.findAllComponents({ name: 'Select' }).find(select => select.props('modelValue') === 7)
    if (!groupSelect) throw new Error('Group select missing')
    groupSelect.vm.$emit('update:modelValue', null)
    const cancel = wrapper.findAll('button').find(button => button.text() === 'common.cancel')
    if (!cancel) throw new Error('Cancel button missing')
    await cancel.trigger('click')
    expect(updateAlertRule).not.toHaveBeenCalled()
    expect(source).toStrictEqual(rule())
    const edit = wrapper.findAll('button').find(button => button.text() === 'common.edit')
    if (!edit) throw new Error('Edit button missing')
    await edit.trigger('click')
    expect(wrapper.get<HTMLInputElement>('input[type="text"]').element.value).toBe('Existing rule')
    expect(wrapper.findAllComponents({ name: 'Select' }).some(select => select.props('modelValue') === 7)).toBe(true)
  })

  it('keeps absent filters undefined when editing and saving', async () => {
    const source = { ...rule(), filters: undefined }
    updateAlertRule.mockResolvedValue(source)
    const wrapper = await openEdit(source)
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    if (!save) throw new Error('Save button missing')
    await save.trigger('click')
    await flushPromises()
    expect(updateAlertRule.mock.calls[0]?.[1]).toHaveProperty('filters', undefined)
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({
    listGroups: vi.fn().mockResolvedValue([]),
    notifyError: vi.fn(),
    notifySuccess: vi.fn(),
  })
  return { ...actual, useOperationsServices: () => services }
})
