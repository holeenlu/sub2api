import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BulkOpenAICompactMappingPanel from '../BulkOpenAICompactMappingPanel.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('BulkOpenAICompactMappingPanel', () => {
  it('disables keyboard focus and rejects dispatched row edits after deselection', async () => {
    const mappings = Object.freeze([Object.freeze({ from: 'request', to: 'compact' })])
    const host = document.createElement('div')
    document.body.appendChild(host)
    const wrapper = mount(BulkOpenAICompactMappingPanel, { attachTo: host, props: { selected: true, mappings } })
    try {
      await wrapper.setProps({ selected: false })
      const fieldset = wrapper.get<HTMLFieldSetElement>('#bulk-edit-openai-compact-model-mapping')
      const optIn = wrapper.get<HTMLInputElement>('#bulk-edit-openai-compact-model-mapping-enabled')
      expect(fieldset.element.disabled).toBe(true)
      expect(optIn.element.matches(':disabled')).toBe(false)
      optIn.element.focus()
      for (const control of fieldset.findAll<HTMLElement>('input, button')) {
        expect(control.element.matches(':disabled')).toBe(true)
        control.element.focus()
        expect(document.activeElement).toBe(optIn.element)
        control.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
        control.element.dispatchEvent(new KeyboardEvent('keyup', { key: ' ', bubbles: true }))
      }
      for (const input of fieldset.findAll<HTMLInputElement>('input')) {
        input.element.value = 'blocked-model'
        input.element.dispatchEvent(new Event('input', { bubbles: true }))
      }
      for (const button of fieldset.findAll('button')) {
        button.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      }
      expect(wrapper.emitted('update:mappings')).toBeUndefined()
      expect(mappings).toEqual([{ from: 'request', to: 'compact' }])
      await wrapper.setProps({ selected: true })
      expect(fieldset.element.disabled).toBe(false)
      const target = wrapper.get<HTMLInputElement>('input[aria-label="admin.accounts.toModel"]')
      target.element.focus()
      expect(document.activeElement).toBe(target.element)
      await target.setValue('accepted-compact')
      expect(wrapper.emitted('update:mappings')).toEqual([[[{ from: 'request', to: 'accepted-compact' }]]])
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

  it('emits independent source/target edits and explicit clearing without touching frozen rows', async () => {
    const mappings = Object.freeze([Object.freeze({ from: 'gpt-request', to: 'gpt-compact' })])
    const wrapper = mount(BulkOpenAICompactMappingPanel, { props: { selected: true, mappings } })
    wrappers.push(wrapper)
    await wrapper.get('input[aria-label="admin.accounts.fromModel"]').setValue('gpt-new-request')
    expect(wrapper.emitted('update:mappings')).toEqual([[[{ from: 'gpt-new-request', to: 'gpt-compact' }]]])
    await wrapper.setProps({ mappings: [{ from: 'gpt-new-request', to: 'gpt-compact' }] })
    await wrapper.get('input[aria-label="admin.accounts.toModel"]').setValue('gpt-new-compact')
    expect(wrapper.emitted('update:mappings')?.at(-1)).toEqual([[{ from: 'gpt-new-request', to: 'gpt-new-compact' }]])
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-remove"]').trigger('click')
    expect(wrapper.emitted('update:mappings')?.at(-1)).toEqual([[]])
    expect(mappings).toEqual([{ from: 'gpt-request', to: 'gpt-compact' }])
  })

  it('emits field selection and blank row additions separately', async () => {
    const wrapper = mount(BulkOpenAICompactMappingPanel, { props: { selected: false, mappings: Object.freeze([]) } })
    wrappers.push(wrapper)
    await wrapper.get('#bulk-edit-openai-compact-model-mapping-enabled').setValue(true)
    expect(wrapper.emitted('update:selected')).toEqual([[true]])
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    await wrapper.setProps({ selected: true })
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-add"]').trigger('click')
    expect(wrapper.emitted('update:mappings')).toEqual([[[{ from: '', to: '' }]]])
  })
})
