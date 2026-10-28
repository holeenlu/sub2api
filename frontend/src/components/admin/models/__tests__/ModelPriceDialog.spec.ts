import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import ModelPriceDialog from '../ModelPriceDialog.vue'
import TokenPriceFields from '../TokenPriceFields.vue'
import en from '@/i18n/locales/en'
import type { ModelPrice, PriceEntry } from '@/api/admin/models'

const price = (): ModelPrice => ({ input: 2, output: 10, cache_read: 0.1, modes: { fast: { input: 3, output: 15, cache_read: 0.2 } } })
const entry = (): PriceEntry => ({ id: 'gpt-6.1-sol', source: 'models.dev', effective: price(), override: null })
const dialog = { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' }
function mountDialog(row = entry(), props = {}) {
  return mount(ModelPriceDialog, { props: { entry: row, platform: 'openai', busy: false, image: false, ...props }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })], stubs: { BaseDialog: dialog } } })
}

describe('model price editing', () => {
  it('keeps edits in the dialog draft and preserves hidden platform modes when saving', async () => {
    const row = entry(); const wrapper = mountDialog(row)
    await wrapper.get('[data-price-field="input"]').setValue('0')
    expect(row.effective?.input).toBe(2)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]?.[0]).toMatchObject({ input: 0, output: 10, modes: price().modes })
  })
  it('collapses unused context tiers and keeps existing tiers expanded', async () => {
    const wrapper = mountDialog()
    expect(wrapper.get('details').attributes('open')).toBeUndefined()
    const row = entry(); row.effective!.tiers = [{ input: null, output: null, cache_read: null, above: 200000, unpriced: true }]
    await wrapper.setProps({ entry: row })
    expect(wrapper.get('details').attributes('open')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]?.[0]).toMatchObject({ tiers: row.effective!.tiers })
  })
  it('preserves the empty image band as inheritance and partial fields require completion', async () => {
    const wrapper = mountDialog(entry(), { image: true })
    const image = wrapper.findAllComponents(TokenPriceFields).find(component => component.props('image'))!
    expect(image.findAll('input').every(input => input.attributes('required') === undefined)).toBe(true)
    await image.get('[data-price-field="input"]').setValue('8')
    expect(image.findAll('input').every(input => input.attributes('required') !== undefined)).toBe(true)
    await image.get('[data-price-field="input"]').setValue('')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]?.[0]).toMatchObject({ image: null })
  })
  it('locks the fields and actions while saving', () => {
    const wrapper = mountDialog(entry(), { busy: true })
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').filter(button => !button.element.closest('fieldset')).every(button => button.attributes('disabled') !== undefined)).toBe(true)
  })
  it('emits a separate reset without mutating the supplied override', async () => {
    const row = entry(); row.override = price(); const wrapper = mountDialog(row)
    const reset = wrapper.findAll('button').find(button => button.text() === 'Reset override')!
    await reset.trigger('click')
    expect(wrapper.emitted('save')).toEqual([[null]])
    expect(row.override).toEqual(price())
  })
})
