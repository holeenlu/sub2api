import { createHash } from 'node:crypto'
import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { compileStyle, parse } from 'vue/compiler-sfc'
import { ModelIcon } from '../../modelIcon'
import source from '../ModelIcon.vue?raw'

enableAutoUnmount(afterEach)

// Frozen from the retiring component: these are artwork digests, not a second classifier.
const artwork = [
  { model: 'claude-opus-4', color: '#D97706', paths: 'c961e8595c0a0ccdab3a923de82b0185bc65649cf6eda3f4ed065534690ae7fc' },
  { model: 'gpt-4o', color: '#000000', paths: '7cc7669776c596e2be6a634487372a9c75890d80d0a32eaa823163c9f27666a7' },
  { model: 'gemini-2.5-pro', color: '#4285F4', paths: '5d77be5701e045b2a9b28487c60dd0823da5070c9a09c7b375028dab6987c8a2' },
  { model: 'glm-4', color: '#3859FF', paths: '989dfbcfa823c08c13bcf10d32676d0f5b36006d2d9efb01f352fad66ac14c10' },
  { model: 'qwen3', color: '#615EFF', paths: 'aa4d68fca4c2f613440ff6b66a1321bfbed9cf79d980cd1cbdb6dcfcc8a40309' },
  { model: 'deepseek-r1', color: '#4D6BFE', paths: '487998c547c0b641b68c815d6ddd885bceb0c3af17f259a76f69817e46a0c497' },
  { model: 'mistral-large', color: '#F7D046', paths: '402b7430b51974925d11d779b04963eb0a7b2b77dae054164afcbfb4ec06716b' },
  { model: 'llama-3', color: '#0668E1', paths: 'acd4f291ec570b896f2282179b70cfb43215a8299d4ba0fcd14c475c5d5abc9c' },
  { model: 'command-r', color: '#39594D', paths: '1cc2a05177da30491cf2d2146a252c63e46074bddefa2c2e54f8fabe25879b85' },
  { model: 'yi-large', color: '#003425', paths: '544fbbdf12c902b6fe8c9eec132416a72a7100d6078e656c0d002799fadd7a01' },
  { model: 'grok-3', color: '#000000', paths: 'ef19c488ec0577aad9a93c4ca3fbade0f31100f84b66076f4ffc6597f6db0cce' },
  { model: 'kimi-k2', color: '#16191E', paths: '6419ead4e3d9ca6272ec4f3037226926323b6b02151eaff4a3a907ce0442ce9f' },
  { model: 'doubao-pro', color: '#1C64F2', paths: '786912efb249a9f46fc5bb3bf158ec0a1faa257b70dffb0ff0bc81df5d2b9da1' },
  { model: 'minimax-m1', color: '#F23F5D', paths: 'd237a7d726de5b4ecd58dd128a54261d5f7ecc31cf1e0d2d8e120f29b563aecc' },
  { model: 'ernie-4', color: '#167ADF', paths: '7093e05a93354599ab732fcb26016c79ad1ae87dd5367ceec5fe81d1cda19a63' },
  { model: 'spark-v4', color: '#0070F0', paths: '8c951c779e6572bde7dce8834df5ad32e9fd5a9f33a4e25eb356ad7aff1a7056' },
  { model: 'hunyuan-pro', color: '#0053E0', paths: '6568f62ec8acf1a634f6989007cd84bce7d811a80660aecc8b8d35893bfddeea' },
  { model: '@cf/custom-model', color: '#F38020', paths: '7c8de61d530143dfcab9babe4732835fc73208b51b1828b583803c0bfcacb0db' },
  { model: 'mj_v6', color: '#000000', paths: 'ed58a4dbbf73810b7534a8e5687b392c80e204a06c6f4f7d55126b092d9985ed' },
  { model: 'pplx-online', color: '#22B8CD', paths: 'bcd1a2d6e31f9c46d7596ffbb330aec472a33847b63b12d216c869d33e12e409' },
  { model: 'jina-reranker', color: '#000000', paths: '7abdadc490815a32cea75178f5f943808bb001b90bfc97fb581d510da4aaba91' },
  { model: 'openrouter/auto', color: '#6566F1', paths: 'bd7541f1da9d595a8f45394313c11f2a12179e1b52ed6c256e92d7fc83b6d63a' },
  { model: 'suno-v4', color: '#000000', paths: '14ecbeb4b9366ff800fd5fa74a13cbc820fb179b90c2a44fa08fe3ac86f8b53b' },
  { model: '360-model', color: '#23B7E5', paths: 'b44182baa68a17d3d1983097c24b23d262f6b8a952d076aa197d8e71e4e37de1' },
  { model: 'dify-chat', color: '#1677FF', paths: 'b44182baa68a17d3d1983097c24b23d262f6b8a952d076aa197d8e71e4e37de1' },
  { model: 'coze-agent', color: '#5436F5', paths: 'b44182baa68a17d3d1983097c24b23d262f6b8a952d076aa197d8e71e4e37de1' },
]

describe('catalog model icon presentation', () => {
  it.each(artwork)('preserves the rendered artwork and color for $model', ({ model, color, paths }) => {
    const wrapper = mount(ModelIcon, { props: { model } })
    const svg = wrapper.get('svg.model-icon')
    expect(svg.attributes()).toMatchObject({ width: '18px', height: '18px', viewBox: '0 0 24 24', fill: 'currentColor', 'fill-rule': 'evenodd' })
    const renderedPaths = svg.findAll('path')
    expect(renderedPaths.length).toBeGreaterThan(0)
    expect(renderedPaths.map(path => path.attributes('fill'))).toEqual(renderedPaths.map(() => color))
    expect(createHash('sha256').update(JSON.stringify(renderedPaths.map(path => path.attributes('d')))).digest('hex')).toBe(paths)
    expect(wrapper.find('.model-icon-fallback').exists()).toBe(false)
  })

  it.each([
    ['GPT-4O', 'gpt-4o'], ['o1-preview', 'gpt-4o'], ['o3-mini', 'gpt-4o'], ['o4-mini', 'gpt-4o'],
    ['ChatGPT-latest', 'gpt-4o'], ['dall-e-3', 'gpt-4o'], ['whisper-1', 'gpt-4o'], ['tts-1', 'gpt-4o'],
    ['text-embedding-3-large', 'gpt-4o'], ['text-moderation-latest', 'gpt-4o'],
    ['babbage-002', 'gpt-4o'], ['davinci-002', 'gpt-4o'], ['curie', 'gpt-4o'], ['ada', 'gpt-4o'],
    ['CLAUDE-OPUS', 'claude-opus-4'], ['gemma-3', 'gemini-2.5-pro'], ['learnlm', 'gemini-2.5-pro'],
    ['imagen-3', 'gemini-2.5-pro'], ['veo-3', 'gemini-2.5-pro'], ['cogview', 'glm-4'], ['cogvideo', 'glm-4'],
    ['qwq-32b', 'qwen3'], ['mixtral', 'mistral-large'], ['codestral', 'mistral-large'],
    ['pixtral', 'mistral-large'], ['voxtral', 'mistral-large'], ['magistral', 'mistral-large'],
    ['c4ai-model', 'command-r'], ['embed-model', 'command-r'], ['yi model', 'yi-large'],
    ['moonshot-v1', 'kimi-k2'], ['abab', 'minimax-m1'], ['wenxin', 'ernie-4'],
    ['midjourney', 'mj_v6'], ['perplexity', 'pplx-online'],
    // Existing precedence is presentation behavior, including Ollama matching Llama first.
    ['gpt-claude', 'gpt-4o'], ['@cf/meta/llama-3', 'llama-3'], ['ollama', 'llama-3'], ['mistral-embed-model', 'mistral-large']
  ])('keeps existing matching and precedence for %s', (model, canonical) => {
    const actual = mount(ModelIcon, { props: { model } })
    const expected = mount(ModelIcon, { props: { model: canonical } })
    expect(actual.html()).toBe(expected.html())
  })

  it.each([['custom-model', 'C'], ['zeta', 'Z'], ['', ''], [' unknown', '']])('keeps the fallback for %j', (model, text) => {
    const wrapper = mount(ModelIcon, { props: { model } })
    expect(wrapper.find('svg').exists()).toBe(false)
    const fallback = wrapper.get('.model-icon-fallback')
    expect(fallback.text()).toBe(text)
    expect(fallback.attributes('style')).toContain('width: 18px')
    expect(fallback.attributes('style')).toContain('height: 18px')
    expect(fallback.attributes('style')).toContain('font-size: calc(')
  })

  it('updates the same instance when its model and size change', async () => {
    const wrapper = mount(ModelIcon, { props: { model: 'gpt-4o', size: '24px' }, attrs: { class: 'consumer-tint', title: 'model label' } })
    expect(wrapper.get('svg').attributes('width')).toBe('24px')
    expect(wrapper.classes()).toContain('consumer-tint')
    expect(wrapper.attributes('title')).toBe('model label')
    await wrapper.setProps({ model: 'claude-opus-4', size: '28px' })
    expect(wrapper.get('path').attributes('fill')).toBe('#D97706')
    expect(wrapper.get('svg').attributes('height')).toBe('28px')
    await wrapper.setProps({ model: 'unknown', size: '14px' })
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.get('span').text()).toBe('U')
    expect(wrapper.get('span').attributes('style')).toContain('width: 14px')
    await wrapper.setProps({ model: '' })
    expect(wrapper.get('span').text()).toBe('')
    await wrapper.setProps({ model: 'gemini-2.5-pro' })
    expect(wrapper.get('path').attributes('fill')).toBe('#4285F4')
  })

  it('retains scoped icon and fallback layout, gradient and typography', async () => {
    const wrapper = mount(ModelIcon, { props: { model: 'gpt-4o' }, attachTo: document.body })
    const scopeId = Object.keys(wrapper.attributes()).find(name => name.startsWith('data-v-'))
    const style = parse(source).descriptor.styles[0]
    if (!scopeId || !style) throw new Error('ModelIcon must retain scoped styles')
    const compiled = compileStyle({ source: style.content, filename: 'ModelIcon.vue', id: scopeId, scoped: true })
    expect(compiled.errors).toEqual([])
    const stylesheet = document.createElement('style')
    stylesheet.textContent = compiled.code
    document.head.append(stylesheet)
    try {
      expect(getComputedStyle(wrapper.element).flexShrink).toBe('0')
      await wrapper.setProps({ model: 'unknown' })
      const computed = getComputedStyle(wrapper.element)
      expect(computed.display).toBe('inline-flex')
      expect(computed.alignItems).toBe('center')
      expect(computed.justifyContent).toBe('center')
      expect(computed.borderRadius).toBe('4px')
      expect(computed.backgroundImage).toContain('linear-gradient(135deg')
      expect(computed.color).toBe('rgb(255, 255, 255)')
      expect(computed.fontWeight).toBe('600')
      expect(computed.flexShrink).toBe('0')
    } finally {
      stylesheet.remove()
    }
  })
})
