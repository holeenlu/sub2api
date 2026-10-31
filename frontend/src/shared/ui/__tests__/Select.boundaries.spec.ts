import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { h, reactive } from 'vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const global = { stubs: { Teleport: true, Transition: true } }

describe('Select option boundaries', () => {
  it.each([0, false, '', null, undefined])('keeps the dynamic value %s and its original option', async value => {
    const option = reactive({ id: value, title: `Label:${String(value)}`, metadata: { count: 3 } })
    const wrapper = mount(Select, {
      props: { modelValue: value, options: [option], valueKey: 'id', labelKey: 'title', clearable: true },
      global,
    })
    expect(wrapper.get('.select-value').text()).toBe(option.title)
    expect(wrapper.find('.select-clear').exists()).toBe(value === 0 || value === false)
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.get('[role="option"]').attributes('aria-selected')).toBe('true')
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[value ?? null]])
    expect(wrapper.emitted('change')?.[0]?.[1]).toBe(option)
  })

  it.each([0, false, ''])('keeps primitive option %s', async value => {
    const wrapper = mount(Select, { props: { modelValue: value, options: [value] }, global })
    expect(wrapper.get('.select-value').text()).toBe(String(value))
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('change')).toEqual([[value, value]])
  })

  it.each([0, false])('shows an unmatched creatable value %s without dropping it', value => {
    const wrapper = mount(Select, {
      props: { modelValue: value, options: [], creatable: true, placeholder: 'Choose' }, global,
    })
    expect(wrapper.get('.select-value').text()).toBe(String(value))
  })

  it.each([null, undefined, ''])('keeps the placeholder for an unmatched empty value %s', value => {
    const wrapper = mount(Select, {
      props: { modelValue: value, options: [], creatable: true, placeholder: 'Choose' }, global,
    })
    expect(wrapper.get('.select-value').text()).toBe('Choose')
  })

  it('exposes original options to slots and emits null when clearing a falsy selection', async () => {
    const option = reactive({ id: false, title: 'Disabled feature', detail: { count: 2 } })
    const wrapper = mount(Select<typeof option>, {
      props: { modelValue: false, options: [option], valueKey: 'id', labelKey: 'title', clearable: true },
      slots: {
        selected: ({ option: selected }) => h('span', { 'data-test': 'selected' }, String(selected?.detail.count)),
        option: ({ option: item, selected }) => h('span', { 'data-test': 'option' }, `${item === option}:${selected}`),
      },
      global,
    })
    expect(wrapper.get('[data-test="selected"]').text()).toBe('2')
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.get('[data-test="option"]').text()).toBe('true:true')
    await wrapper.get('.select-clear').trigger('click')
    expect(wrapper.emitted('change')).toEqual([[null, null]])
  })

  it('creates an option using dynamic keys without inventing custom fields', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null, options: [{ id: 2, title: 'Beta', description: 'needle' }],
        valueKey: 'id', labelKey: 'title', searchable: true, creatable: true, creatablePrefix: 'Find',
      },
      slots: { option: ({ option }) => h('span', JSON.stringify(option)) },
      global,
    })
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('input').setValue('  needle  ')
    expect(wrapper.get('[role="option"]').text()).toBe(JSON.stringify({ id: 'needle', title: 'Find "needle"', _creatable: true }))
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('change')).toEqual([['needle', { id: 'needle', title: 'Find "needle"', _creatable: true }]])
  })

  it('keeps description search and skips disabled group headers with mouse and keyboard', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null, searchable: true,
        options: [
          { value: 'group', label: 'Group', kind: 'group', disabled: true, description: 'match' },
          { value: 'disabled', label: 'Disabled', disabled: true, description: 'match' },
          { value: false, label: 'Available', description: 'match' },
          { value: 'other', label: 'Other' },
        ],
      },
      global,
    })
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('input').setValue('match')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(3)
    const group = wrapper.get('.select-option-group')
    expect(group.attributes('aria-disabled')).toBe('true')
    await group.trigger('click')
    expect(wrapper.emitted('change')).toBeUndefined()
    await wrapper.get('[role="listbox"]').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.get('[role="listbox"]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it.each([{}, [], Symbol('invalid'), () => 3])('does not emit unsupported dynamic values (%s)', async id => {
    const wrapper = mount(Select, {
      props: { modelValue: null, options: [{ id, title: 'Invalid' }], valueKey: 'id', labelKey: 'title' }, global,
    })
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('change')).toBeUndefined()
  })

  it('keeps remote results intact even when creatable is enabled', async () => {
    const wrapper = mount(Select, {
      props: { modelValue: null, options: [{ value: 0, label: 'Remote' }], remote: true, creatable: true }, global,
    })
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('input').setValue('new')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1)
    expect(wrapper.get('[role="option"]').text()).toBe('Remote')
  })
})
