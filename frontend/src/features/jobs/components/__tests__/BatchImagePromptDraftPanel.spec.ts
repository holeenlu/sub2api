import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BatchImagePromptDraftPanel from '../BatchImagePromptDraftPanel.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function mountPanel() {
  return mount(BatchImagePromptDraftPanel, {
    props: {
      prompt: '', customId: '', outputCount: 1,
      referenceNames: Object.freeze(['reference.png']),
      rows: Object.freeze([Object.freeze({
        localId: 'row-a', customId: 'image-a', prompt: 'A landscape', outputCount: 2, referenceCount: 1,
      })]),
      maxOutputsPerItem: 4, maxOutputsPerJob: 200, referenceLimit: 3,
    },
    global: { stubs: { Icon: true } },
  })
}

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('batch image prompt draft panel', () => {
  it('emits typed edits and add intent without mutating the controlled draft', async () => {
    const wrapper = mountPanel()
    try {
      const add = wrapper.get('button.btn-secondary')
      expect(add.attributes('disabled')).toBeDefined()
      expect(wrapper.get('label.input-label').attributes('for')).toBe(wrapper.get('textarea').attributes('id'))
      await wrapper.get('textarea').setValue('  A new scene  ')
      await wrapper.get('input[type="text"]').setValue('my id')
      await wrapper.get('select').setValue('3')
      expect(wrapper.emitted('update:prompt')).toEqual([['  A new scene  ']])
      expect(wrapper.emitted('update:customId')).toEqual([['my id']])
      expect(wrapper.emitted('update:outputCount')).toEqual([[3]])
      expect(wrapper.props()).toMatchObject({ prompt: '', customId: '', outputCount: 1 })
      expect(wrapper.emitted('add-prompt')).toBeUndefined()
      await wrapper.setProps({ prompt: '   ' })
      expect(add.attributes('disabled')).toBeDefined()
      await wrapper.setProps({ prompt: 'A new scene' })
      await add.trigger('click')
      expect(wrapper.emitted('add-prompt')).toEqual([[]])
    } finally { wrapper.unmount() }
  })

  it('hands files to its owner, resets the picker and never reads or uploads them', async () => {
    const read = vi.fn()
    const fetch = vi.fn()
    vi.stubGlobal('FileReader', read)
    vi.stubGlobal('fetch', fetch)
    const wrapper = mountPanel()
    try {
      const file = new File(['image'], 'reference.png', { type: 'image/png' })
      const input = wrapper.get<HTMLInputElement>('input[type="file"]')
      Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
      const reset = vi.spyOn(input.element, 'value', 'set')
      await input.trigger('change')
      await input.trigger('change')
      expect(wrapper.emitted('select-reference-files')).toEqual([[[file]], [[file]]])
      expect(reset).toHaveBeenNthCalledWith(1, '')
      expect(reset).toHaveBeenNthCalledWith(2, '')
      Object.defineProperty(input.element, 'files', { value: [] })
      await input.trigger('change')
      expect(wrapper.emitted('select-reference-files')).toHaveLength(2)
      expect(read).not.toHaveBeenCalled()
      expect(fetch).not.toHaveBeenCalled()
      expect(wrapper.props('referenceNames')).toEqual(['reference.png'])
    } finally { wrapper.unmount() }
  })

  it('uses owner-supplied limits and emits removal by row identity without changing summaries', async () => {
    const wrapper = mountPanel()
    try {
      expect(wrapper.findAll('option')).toHaveLength(4)
      expect(wrapper.text()).toContain('image-a')
      expect(wrapper.text()).toContain('x2')
      expect(wrapper.text()).toContain('batchImage.create.referenceCount')
      await wrapper.get('button[title="common.delete"]').trigger('click')
      await wrapper.get('button[title="batchImage.create.removeReferenceImage"]').trigger('click')
      expect(wrapper.emitted('remove-prompt')).toEqual([['row-a']])
      expect(wrapper.emitted('remove-reference')).toEqual([[0]])
      expect(wrapper.props('rows')).toHaveLength(1)
      expect(wrapper.props('referenceNames')).toHaveLength(1)
      await wrapper.setProps({ referenceLimit: 1 })
      expect(wrapper.get('input[type="file"]').attributes('disabled')).toBeDefined()
      await wrapper.setProps({ referenceNames: [], referenceLimit: 0, rows: [] })
      expect(wrapper.get('input[type="file"]').attributes('disabled')).toBeDefined()
      expect(wrapper.text()).toContain('batchImage.create.noPrompts')
      await wrapper.setProps({ referenceLimit: 3 })
      expect(wrapper.get('input[type="file"]').attributes('disabled')).toBeUndefined()
    } finally { wrapper.unmount() }
  })
})
