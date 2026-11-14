import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BulkErrorCodesPanel from '../BulkErrorCodesPanel.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: VueWrapper[] = []
function mountPanel(props: Partial<InstanceType<typeof BulkErrorCodesPanel>['$props']> = {}) {
  const wrapper = mount(BulkErrorCodesPanel, { props: { selected: true, codes: [], input: null, ...props } })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('BulkErrorCodesPanel', () => {
  it.each([418, 401, null, 429, 529])('ignores detached keyboard/input/click handlers after deselection (input=%s)', async input => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const codes = Object.freeze([401])
    const wrapper = mountPanel({ codes, input })
    const retainedInput = wrapper.get<HTMLInputElement>('#bulk-edit-custom-error-code-input').element
    const retainedButtons = wrapper.get('#bulk-edit-custom-error-codes-body').findAll('button').map(button => button.element)
    await wrapper.setProps({ selected: false })
    expect(wrapper.find('#bulk-edit-custom-error-codes-body').exists()).toBe(false)
    retainedInput.value = '503'
    retainedInput.dispatchEvent(new Event('input', { bubbles: true }))
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    retainedInput.dispatchEvent(enter)
    expect(enter.defaultPrevented).toBe(true)
    for (const button of retainedButtons) button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('update:input')).toBeUndefined()
    expect(wrapper.emitted('update:codes')).toBeUndefined()
    expect(wrapper.emitted('info')).toBeUndefined()
    expect(wrapper.emitted('error')).toBeUndefined()
    expect(confirm).not.toHaveBeenCalled()
    expect(codes).toEqual([401])
  })

  it('sorts a display copy and emits removals without mutating frozen selections', async () => {
    const codes = Object.freeze([503, 401])
    const wrapper = mountPanel({ codes })
    expect(wrapper.findAll('[data-testid^="bulk-error-code-remove-"]').map(button => button.attributes('data-testid')))
      .toEqual(['bulk-error-code-remove-401', 'bulk-error-code-remove-503'])
    expect(wrapper.emitted('update:codes')).toBeUndefined()
    await wrapper.get('[data-testid="bulk-error-code-remove-401"]').trigger('click')
    expect(wrapper.emitted('update:codes')).toEqual([[[503]]])
    await wrapper.get('[data-testid="bulk-error-code-toggle-503"]').trigger('click')
    expect(wrapper.emitted('update:codes')?.at(-1)).toEqual([[401]])
    expect(codes).toEqual([503, 401])
  })

  it.each([429, 529])('requires confirmation for %s through both common and manual entry', async code => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const codes = Object.freeze([401])
    const wrapper = mountPanel({ codes, input: code })
    await wrapper.get(`[data-testid="bulk-error-code-toggle-${code}"]`).trigger('click')
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(confirm.mock.calls).toEqual([
      [`admin.accounts.customErrorCodes${code}Warning`], [`admin.accounts.customErrorCodes${code}Warning`]
    ])
    expect(wrapper.emitted('update:codes')).toBeUndefined()
    expect(wrapper.emitted('update:input')).toBeUndefined()
    confirm.mockReturnValue(true)
    await wrapper.get(`[data-testid="bulk-error-code-toggle-${code}"]`).trigger('click')
    expect(wrapper.emitted('update:codes')).toEqual([[[401, code]]])
    await wrapper.get('#bulk-edit-custom-error-code-input').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:codes')?.at(-1)).toEqual([[401, code]])
    expect(wrapper.emitted('update:input')).toEqual([[null]])
    await wrapper.setProps({ codes: [401, code] })
    confirm.mockClear()
    await wrapper.get(`[data-testid="bulk-error-code-toggle-${code}"]`).trigger('click')
    expect(wrapper.emitted('update:codes')?.at(-1)).toEqual([[401]])
    expect(confirm).not.toHaveBeenCalled()
    expect(codes).toEqual([401])
  })

  it.each([null, '', 99, 600, 401.5])('rejects invalid manual input %s without clearing the draft', async input => {
    const wrapper = mountPanel({ input })
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(wrapper.emitted('error')).toEqual([['admin.accounts.invalidErrorCode']])
    expect(wrapper.emitted('update:codes')).toBeUndefined()
    expect(wrapper.emitted('update:input')).toBeUndefined()
  })

  it('reports duplicates and exposes typed input updates', async () => {
    const wrapper = mountPanel({ codes: Object.freeze([418]), input: 418 })
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(wrapper.emitted('info')).toEqual([['admin.accounts.errorCodeExists']])
    expect(wrapper.emitted('update:codes')).toBeUndefined()
    await wrapper.get('#bulk-edit-custom-error-code-input').setValue('422')
    expect(wrapper.emitted('update:input')).toEqual([[422]])
    await wrapper.setProps({ input: 422 })
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(wrapper.emitted('update:codes')).toEqual([[[418, 422]]])
    expect(wrapper.emitted('update:input')?.at(-1)).toEqual([null])
  })

  it('keeps field opt-in separate from empty/default codes', async () => {
    const wrapper = mountPanel({ selected: false })
    expect(wrapper.find('#bulk-edit-custom-error-codes-body').exists()).toBe(false)
    await wrapper.get('#bulk-edit-custom-error-codes-enabled').setValue(true)
    expect(wrapper.emitted('update:selected')).toEqual([[true]])
    await wrapper.setProps({ selected: true })
    expect(wrapper.text()).toContain('admin.accounts.noneSelectedUsesDefault')
    expect(wrapper.emitted('update:codes')).toBeUndefined()
  })
})
