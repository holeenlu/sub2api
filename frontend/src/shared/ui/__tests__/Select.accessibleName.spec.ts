import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import Select from '../Select.vue'

enableAutoUnmount(afterEach)

describe('Select accessible names', () => {
  it('updates its default name with the active language without changing the selected value', async () => {
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, zh } })
    const wrapper = mount(Select, {
      props: { modelValue: 'hour', options: [{ value: 'hour', label: 'Hour' }] },
      global: { plugins: [i18n] }
    })
    expect(wrapper.get('button').attributes('aria-label')).toBe(en.common.selectOption)
    i18n.global.locale.value = 'zh'
    await nextTick()
    expect(wrapper.get('button').attributes('aria-label')).toBe(zh.common.selectOption)
    expect(wrapper.get('button').text()).toContain('Hour')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('uses field context from the placeholder while preserving an explicit label override', async () => {
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, zh } })
    const wrapper = mount(Select, {
      props: { modelValue: null, options: [], placeholder: 'Choose platform' },
      global: { plugins: [i18n] }
    })
    expect(wrapper.get('button').attributes('aria-label')).toBe('Choose platform')
    await wrapper.setProps({ ariaLabel: 'Platform filter', placeholder: 'Choose another platform' })
    expect(wrapper.get('button').attributes('aria-label')).toBe('Platform filter')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
