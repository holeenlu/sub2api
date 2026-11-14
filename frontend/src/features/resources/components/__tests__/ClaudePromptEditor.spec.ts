import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ClaudePromptEditor from '../ClaudePromptEditor.vue'
import { claudePromptPresetBlocks, serializeClaudePromptBlocks } from '@/features/resources/model/claudePrompt'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function mountEditor(raw = '', enabled = true) {
  const wrapper = mount(ClaudePromptEditor, {
    props: {
      modelValue: raw, enabled,
      'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) },
      'onUpdate:enabled': value => { void wrapper.setProps({ enabled: value }) }
    }
  })
  return wrapper
}

describe('Claude prompt editor', () => {
  it('switches layouts, exposes the actual template and reads zero back without adding blocks', async () => {
    const wrapper = mountEditor()
    for (const preset of ['official', 'official_full', 'minimal', 'zero'] as const) {
      await wrapper.get('[data-testid="claude-prompt-preset"]').setValue(preset)
      await flushPromises()
      const saved = wrapper.props('modelValue')
      expect(JSON.parse(saved).map((block: { text: string }) => block.text)).toEqual(claudePromptPresetBlocks(preset).map(block => block.text))
      const restored = mountEditor(saved)
      expect((restored.get('[data-testid="claude-prompt-preset"]').element as HTMLSelectElement).value).toBe(preset)
      expect(restored.findAll('[data-testid="claude-prompt-block"]')).toHaveLength(claudePromptPresetBlocks(preset).length)
      expect(JSON.parse(restored.get('[data-testid="claude-prompt-preview"]').text())).toHaveLength(claudePromptPresetBlocks(preset).length)
      restored.unmount()
    }
    expect(wrapper.props('enabled')).toBe(true)
    expect(wrapper.props('modelValue')).toBe('[]')
    wrapper.unmount()
  })

  it('keeps edited custom content when switching away and back', async () => {
    const wrapper = mountEditor(serializeClaudePromptBlocks(claudePromptPresetBlocks('official')))
    await wrapper.get('[data-testid="claude-prompt-text"]').setValue('Tenant policy with literal {fp}')
    await flushPromises()
    const custom = wrapper.props('modelValue')
    await wrapper.get('[data-testid="claude-prompt-preset"]').setValue('zero')
    await wrapper.get('[data-testid="claude-prompt-preset"]').setValue('custom')
    await flushPromises()
    expect(wrapper.props('modelValue')).toBe(custom)
    wrapper.unmount()
  })

  it('preserves a saved custom draft when presets are selected before any edits', async () => {
    const wrapper = mountEditor('[{"enabled":true,"type":"text","text":"Saved project policy"}]')
    await wrapper.get('[data-testid="claude-prompt-preset"]').setValue('official')
    await wrapper.get('[data-testid="claude-prompt-preset"]').setValue('custom')
    await flushPromises()
    expect(JSON.parse(wrapper.props('modelValue'))).toEqual([{ enabled: true, type: 'text', text: 'Saved project policy' }])
    wrapper.unmount()
  })

  it('retains invalid JSON for repair and shows validation without silently loading defaults', async () => {
    const wrapper = mountEditor('[]')
    await wrapper.get('[data-testid="claude-prompt-json"]').setValue('[{"placement":"unknown"}]')
    await flushPromises()
    expect(wrapper.props('modelValue')).toBe('[{"placement":"unknown"}]')
    expect(wrapper.find('[data-testid="claude-prompt-error"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="claude-prompt-block"]')).toHaveLength(0)
    await wrapper.get('[data-testid="claude-prompt-json"]').setValue('[]')
    await flushPromises()
    expect(wrapper.find('[data-testid="claude-prompt-error"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows disabled injection as an empty template preview while keeping saved edits', () => {
    const raw = serializeClaudePromptBlocks(claudePromptPresetBlocks('official_full'))
    const wrapper = mountEditor(raw, false)
    expect(wrapper.get('[data-testid="claude-prompt-preview"]').text()).toBe('[]')
    expect(wrapper.props('modelValue')).toBe(raw)
    expect(wrapper.findAll('[data-testid="claude-prompt-block"]')).toHaveLength(claudePromptPresetBlocks('official_full').length)
    wrapper.unmount()
  })
})
