import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BulkOpenAIWebSocketPanel from '../BulkOpenAIWebSocketPanel.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('BulkOpenAIWebSocketPanel', () => {
  it.each(['bulk-edit-openai-ws-mode', 'bulk-edit-openai-apikey-ws-mode'] as const)(
    'edits each mode through the real selector with independent opt-in: %s', async prefix => {
      const wrapper = mount(BulkOpenAIWebSocketPanel, {
        props: { prefix, selected: false, mode: 'off' },
        global: { stubs: { Teleport: true } }
      })
      wrappers.push(wrapper)
      expect(wrapper.get('button.select-trigger').attributes('disabled')).toBeDefined()
      wrapper.getComponent({ name: 'Select' }).vm.$emit('update:modelValue', 'http_bridge')
      expect(wrapper.emitted('update:mode')).toBeUndefined()
      await wrapper.get(`#${prefix}-enabled`).setValue(true)
      expect(wrapper.emitted('update:selected')).toEqual([[true]])
      expect(wrapper.emitted('update:mode')).toBeUndefined()
      await wrapper.setProps({ selected: true })
      for (const [mode, label, hint] of [
        ['ctx_pool', 'wsModeCtxPool', 'wsModeCtxPoolHint'],
        ['passthrough', 'wsModePassthrough', 'wsModePassthroughHint'],
        ['http_bridge', 'wsModeHttpBridge', 'wsModeHttpBridgeHint'],
        ['off', 'wsModeOff', null]
      ] as const) {
        await wrapper.get('button.select-trigger').trigger('click')
        const option = wrapper.findAll('[role="option"]').find(option => option.text() === `admin.accounts.openai.${label}`)
        expect(option).toBeDefined()
        await option?.trigger('click')
        expect(wrapper.emitted('update:mode')?.at(-1)).toEqual([mode])
        await wrapper.setProps({ mode })
        if (hint) expect(wrapper.text()).toContain(`admin.accounts.openai.${hint}`)
        else expect(wrapper.findAll('p')).toHaveLength(1)
      }
      await wrapper.get(`#${prefix}-enabled`).setValue(false)
      expect(wrapper.emitted('update:selected')?.at(-1)).toEqual([false])
    }
  )
})
