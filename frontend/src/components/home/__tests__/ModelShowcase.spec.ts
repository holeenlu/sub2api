import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import ModelShowcase from '../ModelShowcase.vue'
import en from '@/i18n/locales/en/landing'
import zh from '@/i18n/locales/zh/landing'
import zhTW from '@/i18n/locales/zh-TW/landing'
import ja from '@/i18n/locales/ja/landing'

const { copyToClipboard, getModelPlaza } = vi.hoisted(() => ({ copyToClipboard: vi.fn(), getModelPlaza: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza }))

const model = (name = 'claude-fable-5-1') => ({
  name, platform: 'anthropic',
  pricing: { billing_mode: 'token', input_price: 0.000004, output_price: 0.00002, cache_read_price: 0.0000004 },
  official_pricing: { input_price: 99, output_price: 99, cache_read_price: 99 }
})
const group = (id = 1, models = [model()]) => ({ id, name: 'Native group', rate_multiplier: 2, models })
function render(locale = 'en') {
  return mount(ModelShowcase, { global: { plugins: [createPinia(), createI18n({ legacy: false, locale, messages: { en, zh, 'zh-TW': zhTW, ja } })] } })
}
beforeEach(() => { vi.clearAllMocks(); getModelPlaza.mockResolvedValue({ groups: [group()] }) })

describe('ModelShowcase channel data', () => {
  it('uses native group name and model pricing with the group rate', async () => {
    const wrapper = render()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await flushPromises()
    const card = wrapper.get('article')
    expect(card.get('h4').text()).toBe('claude-fable-5-1')
    expect(card.text()).toContain('Native group')
    expect(card.findAll('dd').map(el => el.text())).toEqual(['US$8.00', 'US$40.00', 'US$0.80'])
    expect(card.get('a').attributes('href')).toBe('https://platform.claude.com/docs/en/models/fable-5-1/overview')
  })

  it('uses the first six entries in API order across groups, including repeated models', async () => {
    getModelPlaza.mockResolvedValue({ groups: [group(1, [model(), model('custom')]), group(2, Array.from({ length: 6 }, (_, i) => model(i ? `other-${i}` : 'claude-fable-5-1')))] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('h4').map(el => el.text())).toEqual(['claude-fable-5-1', 'custom', 'claude-fable-5-1', 'other-1', 'other-2', 'other-3'])
    expect(wrapper.text()).not.toContain('home.models.introductions.')
  })

  it.each([0, 0.5])('uses personal multiplier %s without falling back to group rate', async (rate) => {
    getModelPlaza.mockResolvedValue({ groups: [{ ...group(), user_rate_multiplier: rate }] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('dd').map(el => el.text())).toEqual(rate ? ['US$2.00', 'US$10.00', 'US$0.20'] : ['US$0.00', 'US$0.00', 'US$0.00'])
  })

  it('preserves zero prices and shows missing data as unknown', async () => {
    getModelPlaza.mockResolvedValue({ groups: [{ ...group(), name: '', models: [{ ...model(), pricing: { billing_mode: 'token', input_price: 0, output_price: null, cache_read_price: null } }] }] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('dd').map(el => el.text())).toEqual(['US$0.00', '—', '—'])
    expect(wrapper.text()).toContain('Channel name unavailable')
  })

  it('does not label per-request prices as token rates', async () => {
    getModelPlaza.mockResolvedValue({ groups: [group(1, [{ ...model(), pricing: { ...model().pricing, billing_mode: 'per_request' } }])] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('dd').map(el => el.text())).toEqual(['—', '—', '—'])
  })

  it('shows no static models when API is empty and supports retry on failure', async () => {
    getModelPlaza.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ groups: [] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('article')).toHaveLength(0)
    await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('No public models')
    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(wrapper.findAll('a')).toHaveLength(2)
  })

  it.each(['en', 'zh', 'zh-TW', 'ja'])('keeps fixed introductions in %s', async (locale) => {
    const wrapper = render(locale); await flushPromises()
    expect(wrapper.text()).not.toContain('home.models.')
    expect(wrapper.get('article p.my-3').text().length).toBeGreaterThan(40)
  })

  it('copies the dynamic model ID', async () => {
    getModelPlaza.mockResolvedValue({ groups: [group(1, [model('custom-model')])] })
    const wrapper = render(); await flushPromises()
    await wrapper.get('button[aria-label="Copy model ID: custom-model"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('custom-model')
  })
})
