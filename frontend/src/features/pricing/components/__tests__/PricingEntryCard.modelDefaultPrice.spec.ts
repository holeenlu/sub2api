import { ref } from 'vue'
import { pricingScopeKey } from '../../ports'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../../model/pricingForm'
import channelsAPI from '@/features/pricing/api/channels'

vi.mock('@/features/pricing/api/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const getModelDefaultPricing = vi.mocked(channelsAPI.getModelDefaultPricing)

function createEntry(models: string[] = []): PricingFormEntry {
  return {
    models,
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: false,
      periods: [],
    },
  }
}

function addModels(entry: PricingFormEntry, models: string[]) {
  const wrapper = shallowMount(PricingEntryCard, { props: { entry }, global: { provide: { [pricingScopeKey as symbol]: ref('admin:1') } } })
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
  return wrapper
}

const emittedEntries = (wrapper: ReturnType<typeof shallowMount>) =>
  (wrapper.emitted('update') ?? []).map(e => e[0] as PricingFormEntry)

describe('PricingEntryCard default price auto-fill', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getModelDefaultPricing.mockResolvedValue({ found: true, input_price: 3e-6, output_price: 15e-6 })
  })

  it('auto-fills the default price when a model is added to an empty entry', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledTimes(1)
    expect(getModelDefaultPricing).toHaveBeenCalledWith('claude-opus-4-8', expect.any(AbortSignal))
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-8'],
      input_price: 3,
      output_price: 15,
    })
  })

  it('does not overwrite prices when appending a model to an entry that already has one', async () => {
    const entry = createEntry(['claude-opus-4-7'])
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
    })
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBeNull()
      expect(emitted.output_price).toBeNull()
      expect(emitted.cache_write_price).toBeNull()
      expect(emitted.cache_read_price).toBeNull()
    }
  })

  it('does not auto-fill a shared price when several models are added to an empty entry at once', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
      input_price: null,
      output_price: null,
    })
  })

  it('keeps manual prices untouched when a model is appended', async () => {
    const entry = { ...createEntry(['claude-opus-4-7']), input_price: '1.5' }
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBe('1.5')
    }
  })
})

describe('late default price responses', () => {
 it('keeps a manually edited price entered while a lookup is pending', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof channelsAPI.getModelDefaultPricing>>) => void
  getModelDefaultPricing.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const wrapper = addModels(createEntry(), ['model'])
  await wrapper.setProps({ entry: { ...createEntry(['model']), input_price: '9.00' } })
  resolve({ found: true, input_price: 0.000003 }); await flushPromises()
  expect(emittedEntries(wrapper)).toHaveLength(1)
  wrapper.unmount()
 })
 it('aborts superseded model lookups and never fills a removed model', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof channelsAPI.getModelDefaultPricing>>) => void
  getModelDefaultPricing.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const wrapper = addModels(createEntry(), ['model'])
  const signal = getModelDefaultPricing.mock.calls.at(-1)?.[1]
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', [])
  expect(signal?.aborted).toBe(true)
  resolve({ found: true, input_price: 0.000003 }); await flushPromises()
  expect(emittedEntries(wrapper).at(-1)?.models).toEqual([])
  expect(emittedEntries(wrapper)).toHaveLength(2)
  wrapper.unmount()
 })
 it('drops a completion after the editor is disposed', async () => {
  let resolve!: (value: Awaited<ReturnType<typeof channelsAPI.getModelDefaultPricing>>) => void
  getModelDefaultPricing.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const wrapper = addModels(createEntry(), ['model'])
  const signal = getModelDefaultPricing.mock.calls.at(-1)?.[1]
  wrapper.unmount(); expect(signal?.aborted).toBe(true)
  const count = emittedEntries(wrapper).length
  resolve({ found: true, input_price: 0.000003 }); await flushPromises()
  expect(emittedEntries(wrapper)).toHaveLength(count)
 })
})
