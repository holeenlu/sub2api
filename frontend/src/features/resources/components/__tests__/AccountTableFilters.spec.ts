import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountTableFilters from '../AccountTableFilters.vue'
import Select from '@/shared/ui/Select.vue'
import SearchInput from '@/shared/ui/SearchInput.vue'
import type { AccountTableFilterValues } from '../../model/accountTableFilters'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('../../services', () => ({
  useResourceEditorServices: () => ({
    catalog: { listPlatforms: () => [{ id: 'openai', display_name: 'OpenAI' }] }
  })
}))

const filters: Readonly<AccountTableFilterValues> = Object.freeze({
  platform: 'openai', type: 'oauth', status: 'active', privacy_mode: '__unset__', group: '42'
})
const createWrapper = () => mount(AccountTableFilters, {
  props: { searchQuery: 'original search', filters, groups: [{ id: 42, name: 'Group 42' }] }
})
const fields = ['platform', 'type', 'status', 'privacy_mode', 'group'] as const

describe('AccountTableFilters concrete query fields', () => {
  it.each(fields)('emits an immutable %s filter update and preserves the other fields', field => {
    const wrapper = createWrapper()
    const control = wrapper.findAllComponents(Select)[fields.indexOf(field)]
    control.vm.$emit('update:modelValue', 'updated')
    expect(wrapper.emitted('update:filters')).toEqual([[{ ...filters, [field]: 'updated' }]])
    expect(filters[field]).not.toBe('updated')
    expect(wrapper.emitted('change')).toBeUndefined()
    control.vm.$emit('change', 'updated', null)
    expect(wrapper.emitted('change')).toEqual([[]])
    wrapper.unmount()
  })

  it.each(fields)('preserves empty and null clear values for %s', field => {
    const wrapper = createWrapper()
    const control = wrapper.findAllComponents(Select)[fields.indexOf(field)]
    control.vm.$emit('update:modelValue', '')
    control.vm.$emit('update:modelValue', null)
    expect(wrapper.emitted('update:filters')).toEqual([
      [{ ...filters, [field]: '' }], [{ ...filters, [field]: null }]
    ])
    wrapper.unmount()
  })

  it.each(fields)('does not coerce unrelated numeric or boolean Select values into %s', field => {
    const wrapper = createWrapper()
    const control = wrapper.findAllComponents(Select)[fields.indexOf(field)]
    control.vm.$emit('update:modelValue', false)
    control.vm.$emit('update:modelValue', true)
    control.vm.$emit('update:modelValue', 42)
    expect(wrapper.emitted('update:filters')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps string group IDs, the ungrouped sentinel, and privacy-unset distinct', () => {
    const wrapper = createWrapper()
    const controls = wrapper.findAllComponents(Select)
    expect(controls[4].props('options')).toEqual([
      { value: '', label: 'admin.accounts.allGroups' },
      { value: 'ungrouped', label: 'admin.accounts.ungroupedGroup' },
      { value: '42', label: 'Group 42' }
    ])
    controls[4].vm.$emit('update:modelValue', 'ungrouped')
    expect(wrapper.emitted('update:filters')).toEqual([[{ ...filters, group: 'ungrouped' }]])
    expect(controls[3].props('modelValue')).toBe('__unset__')
    wrapper.unmount()
  })

  it('keeps search and reload events separate from filter payloads', () => {
    const wrapper = createWrapper()
    wrapper.findComponent(SearchInput).vm.$emit('update:modelValue', 'new search')
    wrapper.findComponent(SearchInput).vm.$emit('search', 'new search')
    expect(wrapper.emitted('update:searchQuery')).toEqual([['new search']])
    expect(wrapper.emitted('change')).toEqual([[]])
    expect(wrapper.emitted('update:filters')).toBeUndefined()
    wrapper.unmount()
  })
})
