import { describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { uiFeedbackKey, useUiFeedback } from '../feedback'

describe('shared UI feedback injection', () => {
  it('rejects a helper used without an application injection context', () => {
    expect(() => useUiFeedback()).toThrow('UiFeedback is not provided')
  })

  it('rejects missing configuration instead of silently dropping notifications', () => {
    const app = createApp({ render: () => null })
    expect(() => app.runWithContext(() => useUiFeedback())).toThrow('UiFeedback is not provided')
  })

  it('supports installer initialization with provide followed by runWithContext', () => {
    const app = createApp({ render: () => null })
    const feedback = { success: vi.fn(), error: vi.fn() }
    app.provide(uiFeedbackKey, feedback)
    const injected = app.runWithContext(() => useUiFeedback())
    expect(injected).toBe(feedback)
    injected.success('Copied')
    injected.error('Could not copy')
    expect(feedback.success).toHaveBeenCalledWith('Copied')
    expect(feedback.error).toHaveBeenCalledWith('Could not copy')
  })

  it('isolates feedback between component application contexts', () => {
    const first = { success: vi.fn(), error: vi.fn() }
    const second = { success: vi.fn(), error: vi.fn() }
    const Component = defineComponent({
      setup() { return { feedback: useUiFeedback() } },
      template: '<button @click="feedback.error(\'Language failed\')">report</button>',
    })
    const a = mount(Component, { global: { provide: { [uiFeedbackKey as symbol]: first } } })
    const b = mount(Component, { global: { provide: { [uiFeedbackKey as symbol]: second } } })
    a.get('button').element.click()
    expect(first.error).toHaveBeenCalledWith('Language failed')
    expect(second.error).not.toHaveBeenCalled()
    a.unmount()
    b.unmount()
  })
})
