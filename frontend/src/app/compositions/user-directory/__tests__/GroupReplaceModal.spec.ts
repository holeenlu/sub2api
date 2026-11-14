import { useAuthStore } from '@/features/identity/model/auth'
import type { User } from '@/features/identity/api/profileTypes'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { AdminUser } from '@/features/identity/api/profileTypes';
import GroupReplaceModal from '../GroupReplaceModal.vue'

const mocks = vi.hoisted(() => ({ replaceGroup: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/features/access-policy/userAccess', () => ({ replaceGroup: mocks.replaceGroup }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = { id: 800, role: 'super_admin' } as User

  vi.clearAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

async function selectGroup() {
  const wrapper = mount(GroupReplaceModal, {
    props: {
      show: true, user: { id: 10 } as AdminUser, oldGroup: { id: 1, name: 'Old' },
      allGroups: [{ id: 2, name: 'New', status: 'active', is_exclusive: true, subscription_type: 'standard' } as AdminGroup]
    },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, Icon: true
    } }
  })
  await wrapper.get('input[type="radio"]').setValue()
  return wrapper
}

describe('GroupReplaceModal feedback', () => {
  it.each([
    [{ message: 'Group is unavailable' }, 'Group is unavailable'],
    [{ response: { data: { detail: 'Permission denied' } } }, 'Permission denied'],
    [{}, 'common.error']
  ])('shows API errors and keeps the dialog available for retry', async (error, message) => {
    mocks.replaceGroup.mockRejectedValueOnce(error)
    const wrapper = await selectGroup()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith(message)
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeUndefined()
  })

  it('still reports successful migrations and closes', async () => {
    mocks.replaceGroup.mockResolvedValueOnce({ migrated_keys: 3 })
    const wrapper = await selectGroup()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(mocks.replaceGroup).toHaveBeenCalledWith(10, 1, 2, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.users.replaceGroupSuccess')
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
