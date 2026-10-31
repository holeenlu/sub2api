import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { resourceEditorServicesKey } from '../../services'
import BulkModelRestrictionPanel from '../BulkModelRestrictionPanel.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: VueWrapper[] = []
function mountPanel(props: Partial<InstanceType<typeof BulkModelRestrictionPanel>['$props']> = {}) {
  const services = editorTestServices()
  services.managedModelCatalog = vi.fn().mockResolvedValue([{ id: 'new-model', display_name: 'new-model', enabled: true }])
  const wrapper = mount(BulkModelRestrictionPanel, {
    attachTo: document.body,
    props: {
      selected: true, mode: 'mapping', models: [], mappings: [], platforms: ['anthropic'],
      presets: [], disabledByPassthrough: false, ...props
    },
    global: { provide: {
      [resourceEditorServicesKey as symbol]: services,
      [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() }
    } }
  })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => {
    const container = wrapper.element.parentElement
    wrapper.unmount()
    container?.remove()
  })
})

describe('BulkModelRestrictionPanel', () => {
  it('disables keyboard focus and rejects dispatched mapping actions until the field is selected', async () => {
    const mappings = Object.freeze([Object.freeze({ from: 'request', to: 'upstream' })])
    const wrapper = mountPanel({
      selected: false, mappings,
      presets: [
        { label: 'duplicate', from: 'request', to: 'replacement', color: '' },
        { label: 'new', from: 'new-request', to: 'new-upstream', color: '' }
      ]
    })
    const fieldset = wrapper.get<HTMLFieldSetElement>('#bulk-edit-model-restriction-body')
    const optIn = wrapper.get<HTMLInputElement>('#bulk-edit-model-restriction-enabled')
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
    // dispatchEvent deliberately bypasses the browser's disabled-control click suppression.
    for (const button of fieldset.findAll('button')) {
      button.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    }
    for (const input of fieldset.findAll<HTMLInputElement>('input')) {
      input.element.value = 'blocked-model'
      input.element.dispatchEvent(new Event('input', { bubbles: true }))
    }
    expect(wrapper.emitted('update:mode')).toBeUndefined()
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    expect(wrapper.emitted('info')).toBeUndefined()
    expect(mappings).toEqual([{ from: 'request', to: 'upstream' }])

    await optIn.setValue(true)
    expect(wrapper.emitted('update:selected')).toEqual([[true]])
    expect(fieldset.element.disabled).toBe(true)
    await wrapper.setProps({ selected: true })
    expect(fieldset.element.disabled).toBe(false)
    const source = wrapper.get<HTMLInputElement>('input[aria-label="admin.accounts.requestModel"]')
    source.element.focus()
    expect(document.activeElement).toBe(source.element)
    await source.setValue('accepted-model')
    expect(wrapper.emitted('update:mappings')).toEqual([[[{ from: 'accepted-model', to: 'upstream' }]]])
  })

  it('drops keyboard and programmatic whitelist emissions while unselected', async () => {
    const models = Object.freeze(['kept-model'])
    const wrapper = mountPanel({ selected: false, mode: 'whitelist', models })
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('[role="button"]').trigger('keydown', { key: 'Enter' })
    const select = selector.get('[data-testid="select-model"]')
    expect(select.element.matches(':disabled')).toBe(true)
    select.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(selector.emitted('update:modelValue')).toBeUndefined()
    selector.vm.$emit('update:modelValue', ['late-model'])
    expect(wrapper.emitted('update:models')).toBeUndefined()
    expect(models).toEqual(['kept-model'])
    await wrapper.setProps({ selected: true })
    selector.vm.$emit('update:modelValue', ['accepted-model'])
    expect(wrapper.emitted('update:models')).toEqual([[['accepted-model']]])
  })

  it('emits mapping edits, additions and removals without modifying frozen rows', async () => {
    const mappings = Object.freeze([
      Object.freeze({ from: 'request-a', to: 'upstream-a' }),
      Object.freeze({ from: 'request-b', to: 'upstream-b' })
    ])
    const wrapper = mountPanel({ mappings })
    await wrapper.get('input[aria-label="admin.accounts.requestModel"]').setValue('request-c')
    expect(wrapper.emitted('update:mappings')).toEqual([[[
      { from: 'request-c', to: 'upstream-a' }, { from: 'request-b', to: 'upstream-b' }
    ]]])
    await wrapper.get('input[aria-label="admin.accounts.actualModel"]').setValue('upstream-c')
    expect(wrapper.emitted('update:mappings')?.at(-1)).toEqual([[
      { from: 'request-a', to: 'upstream-c' }, { from: 'request-b', to: 'upstream-b' }
    ]])
    await wrapper.get('[data-testid="bulk-model-mapping-add"]').trigger('click')
    expect(wrapper.emitted('update:mappings')?.at(-1)).toEqual([[...mappings, { from: '', to: '' }]])
    await wrapper.get('[data-testid="bulk-model-mapping-remove"]').trigger('click')
    expect(wrapper.emitted('update:mappings')?.at(-1)).toEqual([[{ from: 'request-b', to: 'upstream-b' }]])
    expect(mappings).toEqual([{ from: 'request-a', to: 'upstream-a' }, { from: 'request-b', to: 'upstream-b' }])
  })

  it('adds only the supplied presets and reports duplicate sources without replacing the draft', async () => {
    const existing = Object.freeze({ from: 'request', to: 'custom-upstream' })
    const wrapper = mountPanel({
      mappings: Object.freeze([existing]),
      presets: [{ label: 'existing', from: 'request', to: 'preset-upstream', color: '' }]
    })
    await wrapper.get('[data-testid="bulk-model-mapping-preset"]').trigger('click')
    expect(wrapper.emitted('info')).toEqual([['admin.accounts.mappingExists']])
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    await wrapper.setProps({ presets: [{ label: 'next', from: 'next', to: 'next-upstream', color: '' }] })
    await wrapper.get('[data-testid="bulk-model-mapping-preset"]').trigger('click')
    expect(wrapper.emitted('update:mappings')).toEqual([[[existing, { from: 'next', to: 'next-upstream' }]]])
    expect(existing.to).toBe('custom-upstream')
  })

  it('forwards whitelist interactions and preserves selector inputs across unrelated panel renders', async () => {
    const models = Object.freeze(['kept-model'])
    const mappings = Object.freeze([Object.freeze({ from: 'mapped', to: 'upstream' })])
    const wrapper = mountPanel({ mode: 'whitelist', models, mappings })
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    const selectorModels = selector.props('modelValue')
    const selectorPlatforms = selector.props('platforms')
    expect(selector.props('syncAccountIds')).toBeUndefined()
    await wrapper.setProps({ selected: false })
    expect(selector.props('modelValue')).toBe(selectorModels)
    expect(selector.props('platforms')).toBe(selectorPlatforms)
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    expect(wrapper.emitted('update:selected')).toEqual([[true]])
    await wrapper.setProps({ selected: true })
    await selector.get('[role="button"]').trigger('keydown', { key: 'Enter' }); await flushPromises()
    await selector.get('[data-testid="select-model"]').trigger('click')
    expect(wrapper.emitted('update:models')).toEqual([[['kept-model', 'new-model']]])
    expect(models).toEqual(['kept-model'])
    expect(mappings).toEqual([{ from: 'mapped', to: 'upstream' }])
  })

  it('uses the parent passthrough decision and emits mode changes without erasing either draft', async () => {
    const wrapper = mountPanel({ models: ['kept-model'], mappings: [{ from: 'request', to: 'upstream' }] })
    const source = wrapper.get<HTMLInputElement>('input[aria-label="admin.accounts.requestModel"]').element
    const add = wrapper.get('[data-testid="bulk-model-mapping-add"]').element
    const whitelistButton = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.modelWhitelist'))
    expect(whitelistButton).toBeDefined()
    await whitelistButton?.trigger('click')
    expect(wrapper.emitted('update:mode')).toEqual([['whitelist']])
    await wrapper.setProps({ disabledByPassthrough: true })
    expect(wrapper.get<HTMLFieldSetElement>('#bulk-edit-model-restriction-body').element.disabled).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.openai.modelRestrictionDisabledByPassthrough')
    expect(wrapper.find('[data-testid="bulk-model-mapping-add"]').exists()).toBe(false)
    expect(wrapper.emitted('update:models')).toBeUndefined()
    source.value = 'late-model'
    source.dispatchEvent(new Event('input', { bubbles: true }))
    add.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    await wrapper.setProps({ disabledByPassthrough: false })
    expect(wrapper.get<HTMLInputElement>('input[aria-label="admin.accounts.requestModel"]').element.value).toBe('request')
  })
})
