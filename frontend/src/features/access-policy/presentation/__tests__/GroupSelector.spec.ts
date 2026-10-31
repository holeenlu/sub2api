import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GroupSelector from '../GroupSelector.vue'

import { groupTestContext } from './context'
import type { GroupSelectionOption } from '../types'
const context = groupTestContext()
enableAutoUnmount(afterEach)
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'Basic', platform: 'anthropic' },
  { id: 2, name: 'Composite', platform: 'composite' }
] satisfies GroupSelectionOption[]

const mountSelector = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups },
  global: { provide: context.provide, stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true } }
})

describe('GroupSelector simple-mode binding policy', () => {
  beforeEach(() => { context.isSimpleMode.value = false })

  it('hides composite groups in simple mode and preserves basic groups', () => {
    context.isSimpleMode.value = true
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Basic')
    expect(wrapper.text()).not.toContain('Composite')
  })

  it('keeps composite groups available in advanced mode', () => {
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Composite')
  })

  it('cleans hidden historical composite IDs while preserving visible selections', () => {
    context.isSimpleMode.value = true
    const wrapper = mountSelector([1, 2])
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
  })
  it('reacts when app changes simple mode without mutating readonly selected IDs', async () => {
    const selected = Object.freeze([1, 2])
    const wrapper = mount(GroupSelector, { props: { modelValue: selected, groups }, global: { provide: context.provide } })
    context.isSimpleMode.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
    expect(selected).toEqual([1, 2])
    context.isSimpleMode.value = false
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Composite')
  })

  it('emits new arrays for selection and deselection', async () => {
    const selected = Object.freeze([1])
    const wrapper = mount(GroupSelector, { props: { modelValue: selected, groups }, global: { provide: context.provide } })
    await wrapper.get('input[value="2"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[1, 2]])
    await wrapper.get('input[value="1"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([[]])
    expect(selected).toEqual([1])
  })

  it('retains antigravity mixed-platform filtering and the composite option', () => {
    const groups: GroupSelectionOption[] = [
      { id: 1, name: 'Anthropic', platform: 'anthropic' },
      { id: 2, name: 'Gemini', platform: 'gemini' },
      { id: 3, name: 'Antigravity', platform: 'antigravity' },
      { id: 4, name: 'Composite', platform: 'composite' },
      { id: 5, name: 'OpenAI', platform: 'openai' },
    ]
    const wrapper = mount(GroupSelector, { props: { modelValue: [], groups, platform: 'antigravity', mixedScheduling: true }, global: { provide: context.provide } })
    expect(wrapper.findAll('input[type="checkbox"]').map(input => input.attributes('value'))).toEqual(['1', '2', '3', '4'])
  })

  it('searches names and descriptions when explicitly enabled and accepts minimal projections', async () => {
    const groups = Object.freeze([{ id: 1, name: 'Basic', description: 'Special capacity' }, { id: 2, name: 'Other' }])
    const wrapper = mount(GroupSelector, { props: { modelValue: [], groups, searchable: true }, global: { provide: context.provide } })
    await wrapper.get('input[type="text"]').setValue('SPECIAL')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Basic')
    expect(wrapper.text()).not.toContain('Other')
    expect(wrapper.text()).toContain('—')
  })

  it('keeps automatic search threshold and does not discard IDs while groups have not loaded', async () => {
    context.isSimpleMode.value = true
    const wrapper = mount(GroupSelector, { props: { modelValue: [1], groups: [] }, global: { provide: context.provide } })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ groups: Array.from({ length: 5 }, (_, i) => ({ id: i + 1, name: 'Group ' + i })) })
    expect(wrapper.find('input[type="text"]').exists()).toBe(false)
    await wrapper.setProps({ groups: Array.from({ length: 6 }, (_, i) => ({ id: i + 1, name: 'Group ' + i })) })
    expect(wrapper.find('input[type="text"]').exists()).toBe(true)
  })

})
