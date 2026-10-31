import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import GroupRateField from '../GroupRateField.vue'
import GroupMediaPricingPanel from '../GroupMediaPricingPanel.vue'
import { buildGroupPricingPayload, createGroupPricingDraft } from '../../groupEditor'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
describe('pricing-owned Group drafts and controls', () => {
  it('publishes a zero rate without mutating the readonly source', async () => {
    const source = Object.freeze({ rate_multiplier: 1 })
    const wrapper = mount(GroupRateField, { props: { modelValue: source, editable: () => true } })
    await wrapper.get('input').setValue('0')
    expect(wrapper.emitted('patch')).toEqual([[{ rate_multiplier: 0 }]])
    expect(source.rate_multiplier).toBe(1)
  })
  it('consumes admission facts through props/slots without owning their edits', async () => {
    const source = createGroupPricingDraft()
    const wrapper = mount(GroupMediaPricingPanel, { props: { modelValue: source, platform: 'gemini', imageAllowed: true, batchImagesAllowed: true, editable: () => true },
      slots: { 'image-admission': '<span data-admission>Admission owner</span>', 'batch-admission': '<span data-batch>Batch owner</span>' } })
    expect(wrapper.find('[data-admission]').exists()).toBe(true)
    expect(wrapper.find('[data-batch]').exists()).toBe(true)
    await wrapper.get('input[type="checkbox"]').setValue(true)
    expect(wrapper.emitted('patch')).toEqual([[{ image_rate_independent: true }]])
    expect(source.image_rate_independent).toBe(false)
    await wrapper.setProps({ imageAllowed: false, batchImagesAllowed: false })
    expect(wrapper.find('[data-batch]').exists()).toBe(false)
    expect(wrapper.emitted('patch')).toHaveLength(1)
  })
  it('projects only pricing fields and keeps create-null/update-clear and unknown video families', () => {
    const value = Object.assign(createGroupPricingDraft(), {
      platform: 'grok', is_exclusive: true, subscription_type: 'subscription', daily_limit_usd: 9,
      image_price_1k: 0, video_model_prices: { 'future-family': { '1440p': 0 } },
    })
    const context = { platform: 'grok' as const, imageGenerationAllowed: true, openAIFastSupported: false, batchImageAllowed: false }
    const create = buildGroupPricingPayload(value, context, false)
    const update = buildGroupPricingPayload(value, context, true)
    expect(create).toMatchObject({ image_price_1k: 0, image_price_2k: null, video_model_prices: { 'future-family': { '1440p': 0 } } })
    expect(update.image_price_2k).toBe(-1)
    for (const field of ['platform', 'is_exclusive', 'subscription_type', 'daily_limit_usd', 'profit_min_margin_percent']) expect(update).not.toHaveProperty(field)
    expect(value.image_price_2k).toBeNull()
  })
  it('publishes only the per-image billing patch and respects readonly field access', async () => {
    const source = Object.freeze(createGroupPricingDraft())
    const wrapper = mount(GroupMediaPricingPanel, { props: { modelValue: source, platform: 'openai', imageAllowed: true, batchImagesAllowed: false, editable: () => true } })
    const input = wrapper.get<HTMLInputElement>('[data-testid="group-image-count-billing"]')
    await input.setValue(true)
    expect(wrapper.emitted('patch')).toEqual([[{ image_count_billing: true }]])
    expect(source.image_count_billing).toBe(false)
    await wrapper.setProps({ editable: field => field !== 'image_count_billing' })
    expect(input.element.disabled).toBe(true)
    expect(input.element.closest('label')?.getAttribute('aria-disabled')).toBe('true')
    await wrapper.setProps({ imageAllowed: false })
    expect(wrapper.find('[data-testid="group-image-count-billing"]').exists()).toBe(false)
  })
})
