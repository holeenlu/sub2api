import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import GroupCatalogSources from '../GroupCatalogSources.vue'

const { getGroup, updateGroup, validate } = vi.hoisted(() => ({
  getGroup: vi.fn(), updateGroup: vi.fn(), validate: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: { getById: getGroup, update: updateGroup },
  accounts: { getById: vi.fn().mockResolvedValue({ id: 17, name: 'Source' }) },
} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const Field = defineComponent({
  props: ['modelValue', 'groupId', 'accountNames'], emits: ['update:modelValue'],
  setup(_, { expose }) { expose({ validate }) },
  template: '<button type="button" @click="$emit(\'update:modelValue\', {enabled:true,account_ids:[17],fallback_to_scheduler:true})">change</button>',
})
const mountSources = () => mount(GroupCatalogSources, { props: { initialGroupId: '7' }, global: { stubs: { CodexManifestAccountsField: Field } } })

describe('GroupCatalogSources', () => {
  beforeEach(() => {
    getGroup.mockReset().mockResolvedValue({ id: 7, name: 'Group', platform: 'openai', codex_models_manifest_config: { enabled: false, account_ids: [], fallback_to_scheduler: false } })
    updateGroup.mockReset().mockResolvedValue({})
    validate.mockReset().mockReturnValue(true)
  })
  it('loads the linked group and saves only its source settings', async () => {
    const wrapper = mountSources()
    await flushPromises()
    expect(getGroup).toHaveBeenCalledWith(7)
    await wrapper.findComponent(Field).get('button').trigger('click')
    await wrapper.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(7, { codex_models_manifest_config: { enabled: true, account_ids: [17], fallback_to_scheduler: true } })
    expect(wrapper.get('[role="status"]').text()).toBe('modelCatalog.saved')
  })
  it('does not save invalid sources or reuse a group after the ID changes', async () => {
    const wrapper = mountSources()
    await flushPromises()
    validate.mockReturnValue(false)
    await wrapper.findAll('form')[1].trigger('submit')
    expect(updateGroup).not.toHaveBeenCalled()
    await wrapper.get('input').setValue('8')
    expect(wrapper.findComponent(Field).exists()).toBe(false)
  })
})
