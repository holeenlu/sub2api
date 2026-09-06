import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelShowcase from '../ModelShowcase.vue'
import { SAMPLE_MODELS } from '../sampleModels'

const getModelPlaza = vi.fn()

vi.mock('@/api/modelPlaza', () => ({
  getModelPlaza: () => getModelPlaza()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      // 插值键在断言里要能看出 provider 有带进去
      t: (key: string, named?: Record<string, unknown>) =>
        named ? `${key}:${Object.values(named).join(',')}` : key,
      locale: { value: 'en' }
    })
  }
})

describe('ModelShowcase', () => {
  it('广场回传空清单时，改渲染六张示意卡片与说明', async () => {
    getModelPlaza.mockResolvedValueOnce({ groups: [] })

    const wrapper = mount(ModelShowcase)
    await flushPromises()

    const cards = wrapper.findAll('[data-testid="sample-model-card"]')
    expect(cards).toHaveLength(SAMPLE_MODELS.length)
    expect(cards).toHaveLength(6)

    const first = cards[0].text()
    expect(first).toContain('claude-sonnet-4.5')
    expect(first).toContain('home.models.sampleGroup:Claude')
    expect(first).toContain('US$3.00')
    expect(first).toContain('US$15.00')
    // 每张示意卡片都挂「示意资料」标签
    for (const card of cards) {
      expect(card.text()).toContain('home.management.demoLabel')
    }

    expect(wrapper.get('[data-testid="sample-model-note"]').text()).toBe('home.models.sampleNote')
  })

  it('载入失败时同样退回示意卡片', async () => {
    getModelPlaza.mockRejectedValueOnce(new Error('boom'))

    const wrapper = mount(ModelShowcase)
    await flushPromises()

    expect(wrapper.findAll('[data-testid="sample-model-card"]')).toHaveLength(6)
  })

  it('有真实模型时优先显示真实模型，不显示示意卡片', async () => {
    getModelPlaza.mockResolvedValueOnce({
      groups: [
        {
          id: 1,
          name: 'default',
          rate_multiplier: 2,
          models: [
            {
              name: 'real-model',
              platform: 'openai',
              pricing: { input_price: 0.000_001, output_price: 0.000_002 }
            }
          ]
        }
      ]
    })

    const wrapper = mount(ModelShowcase)
    await flushPromises()

    expect(wrapper.find('[data-testid="sample-model-card"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('real-model')
    // 单价 × 倍率，按 $ / 1M token
    expect(wrapper.text()).toContain('$2.00')
    expect(wrapper.text()).toContain('$4.00')
  })
})
