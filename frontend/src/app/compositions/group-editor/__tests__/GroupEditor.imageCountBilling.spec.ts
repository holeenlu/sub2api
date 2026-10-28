import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { ref } from 'vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes'
import { accessPolicyAccountsKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyScopeKey } from '@/features/access-policy/services'
import GroupEditorDialog from '../GroupEditorDialog.vue'
import { createGroupEditorDraft } from '../model/groupDraft'

const { create, update, getAll, showSuccess, showError } = vi.hoisted(() => ({
  create: vi.fn(), update: vi.fn(), getAll: vi.fn(), showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/features/access-policy/api/groups', () => ({ groupsAPI: { create, update, getAll } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  getAll.mockResolvedValue([])
  create.mockResolvedValue({ id: 43 })
  update.mockResolvedValue({ id: 42 })
})

function storedGroup(patch: Partial<AdminGroup> = {}): AdminGroup {
  return {
    ...createGroupEditorDraft(), id: 42, name: 'Images', platform: 'openai',
    allow_image_generation: true, model_pricing: [], profit_min_margin: 0, profit_safety_buffer: 0,
    model_routing: null, sort_order: 1, created_at: '2026-10-25T00:00:00Z', updated_at: '2026-10-25T00:00:00Z',
    ...patch,
  }
}

function render(group: AdminGroup | null, writable = true) {
  const scope = ref('actor:admin:1')
  const imageWritable = ref(writable)
  const filter = vi.fn((_schema: string, payload: object) => {
    const filtered = { ...payload }
    if (!imageWritable.value) Reflect.deleteProperty(filtered, 'image_count_billing')
    return filtered
  })
  const wrapper = shallowMount(GroupEditorDialog, {
    props: { group },
    global: {
      provide: {
        [accessPolicyScopeKey as symbol]: scope,
        [accessPolicyNotificationsKey as symbol]: { showSuccess, showError },
        [accessPolicyPermissionsKey as symbol]: {
          isSuperAdmin: false, isSimpleMode: false, can: () => true,
          canEditAdminField: (_schema: string, field: string) => field !== 'image_count_billing' || imageWritable.value,
          editableAdminFields: filter,
        },
        [accessPolicyAccountsKey as symbol]: { getById: vi.fn(), search: vi.fn() },
      },
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        GroupMediaPricingPanel: false,
        GroupImageAdmissionField: false,
        ReasoningEffortPolicyFields: false,
      },
    },
  })
  return { wrapper, scope, imageWritable, filter }
}

const imageCountSelector = '[data-testid="group-image-count-billing"]'
type EditorWrapper = ReturnType<typeof render>['wrapper']
async function choosePlatform(wrapper: EditorWrapper, platform: AdminGroup['platform']) {
  wrapper.getComponent({ name: 'Select' }).vm.$emit('update:modelValue', platform)
  await flushPromises()
}
async function allowImages(wrapper: EditorWrapper, allowed: boolean) {
  await wrapper.getComponent({ name: 'GroupImageAdmissionField' }).get('input').setValue(allowed)
}

describe('Group per-image billing in the real atomic editor', () => {
  it.each(['openai', 'composite'] as const)('creates %s with an explicit opt-in and one POST', async platform => {
    const { wrapper, filter } = render(null)
    await flushPromises()
    wrapper.getComponent({ name: 'GroupIdentityPanel' }).vm.$emit('update:modelValue', { name: 'Images' })
    await choosePlatform(wrapper, platform)
    expect(wrapper.find(imageCountSelector).exists()).toBe(false)
    await allowImages(wrapper, true)
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(false)
    await wrapper.get(imageCountSelector).setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(filter).toHaveBeenCalledWith('group.create', expect.objectContaining({ image_count_billing: true }))
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ platform, allow_image_generation: true, image_count_billing: true }))
    expect(update).not.toHaveBeenCalled()
  })

  it('hydrates an enabled value, clears it explicitly, and does not mutate the source DTO', async () => {
    const group = storedGroup({ image_count_billing: true })
    const { wrapper } = render(group)
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(true)
    await wrapper.get(imageCountSelector).setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledWith(42, expect.objectContaining({ image_count_billing: false }))
    expect(create).not.toHaveBeenCalled()
    expect(group.image_count_billing).toBe(true)
  })

  it('uses the legacy missing-field default without inventing a capability', async () => {
    const group = storedGroup()
    Reflect.deleteProperty(group, 'image_count_billing')
    const { wrapper } = render(group)
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(42, expect.objectContaining({ image_count_billing: false }))
    expect(Object.prototype.hasOwnProperty.call(group, 'image_count_billing')).toBe(false)
  })

  it('clears the opt-in when image admission is disabled and never resurrects it', async () => {
    const { wrapper } = render(storedGroup({ image_count_billing: true }))
    await flushPromises()
    await allowImages(wrapper, false)
    expect(wrapper.find(imageCountSelector).exists()).toBe(false)
    await allowImages(wrapper, true)
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(42, expect.objectContaining({ allow_image_generation: true, image_count_billing: false }))
  })

  it('clears per-image billing across unsupported platform changes', async () => {
    const { wrapper } = render(null)
    await flushPromises()
    await choosePlatform(wrapper, 'openai')
    await allowImages(wrapper, true)
    await wrapper.get(imageCountSelector).setValue(true)
    await choosePlatform(wrapper, 'gemini')
    expect(wrapper.find(imageCountSelector).exists()).toBe(false)
    await choosePlatform(wrapper, 'composite')
    await allowImages(wrapper, true)
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(false)
  })

  it.each(['create', 'update'] as const)('omits a readonly field from the %s write command', async action => {
    const group = action === 'create' ? null : storedGroup({ image_count_billing: true })
    const { wrapper, filter } = render(group, false)
    await flushPromises()
    if (group === null) {
      wrapper.getComponent({ name: 'GroupIdentityPanel' }).vm.$emit('update:modelValue', { name: 'Restricted' })
      await choosePlatform(wrapper, 'openai')
      await allowImages(wrapper, true)
    }
    expect(wrapper.get<HTMLInputElement>(imageCountSelector).element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const schema = group ? 'group.update' : 'group.create'
    expect(filter).toHaveBeenCalledWith(schema, expect.objectContaining({ image_count_billing: group !== null }))
    const payload = group ? update.mock.calls[0][1] : create.mock.calls[0][0]
    expect(payload).not.toHaveProperty('image_count_billing')
  })

  it('rejects a completion after per-image field permission changes', async () => {
    let resolve!: () => void
    update.mockReturnValueOnce(new Promise<void>(yes => { resolve = yes }))
    const { wrapper, imageWritable } = render(storedGroup())
    await flushPromises()
    await wrapper.get(imageCountSelector).setValue(true)
    await wrapper.get('form').trigger('submit')
    imageWritable.value = false
    resolve()
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    expect(update).toHaveBeenCalledTimes(1)
  })

  it('reopens a canceled creation with the original false default', async () => {
    const { wrapper } = render(null)
    await flushPromises()
    await choosePlatform(wrapper, 'openai')
    await allowImages(wrapper, true)
    await wrapper.get(imageCountSelector).setValue(true)
    await wrapper.get('button.btn-secondary').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(create).not.toHaveBeenCalled()
    wrapper.unmount()
    const next = render(null).wrapper
    await flushPromises()
    await choosePlatform(next, 'openai')
    await allowImages(next, true)
    expect(next.get<HTMLInputElement>(imageCountSelector).element.checked).toBe(false)
  })
})
