import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UserEditModal from '../UserEditModal.vue'
import UserCreateModal from '../UserCreateModal.vue'
import Select from '@/components/common/Select.vue'
import type { AdminUser } from '@/types'

const { update, updateUserAttributeValues, showSuccess, showError, auth } = vi.hoisted(() => ({
  update: vi.fn(),
  updateUserAttributeValues: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  auth: { isSuperAdmin: true, isAdmin: true, isSimpleMode: false, can: vi.fn(() => true) }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { update },
    userAttributes: { updateUserAttributeValues }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

// useStepUp pulls in the API client, which needs the real i18n instance.
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const mountModal = (concurrency: number, role: AdminUser['role'] = 'user') => mount(UserEditModal, {
  props: {
    show: true,
    user: { id: 7, email: 'user@example.test', username: 'user', notes: '', role, concurrency, rpm_limit: 0 } as never
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show', 'title'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      Select: true,
      Icon: true,
      UserAttributeForm: true,
      TotpStepUpDialog: true
    }
  }
})

beforeEach(() => {
  update.mockReset()
  updateUserAttributeValues.mockReset()
  showSuccess.mockReset()
  showError.mockReset()
  update.mockResolvedValue({})
  auth.isSuperAdmin = true
  auth.can.mockReset().mockReturnValue(true)
})

describe('UserEditModal concurrency', () => {
  // Regression coverage for issue #5977: the gateway treats concurrency <= 0 as
  // unlimited (AcquireUserSlot) and both the batch limits endpoint and the bulk
  // edit modal accept 0, so this dialog must not be the only place that rejects
  // it — doing so blocked every other edit on such a user.
  it('saves an unlimited (0) concurrency instead of blocking the whole form', async () => {
    const wrapper = mountModal(0)

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ concurrency: 0 }))
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('still rejects a negative concurrency', async () => {
    const wrapper = mountModal(3)

    await wrapper.get('[data-test="concurrency-input"]').setValue('-1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.users.concurrencyNonNegative')
    expect(update).not.toHaveBeenCalled()
  })
})

describe('user management roles', () => {
  it('orders editing roles consistently and disables supplier assignment', () => {
    const options = mountModal(1).getComponent(Select).props('options')
    expect(options.map(option => option.value)).toEqual(['super_admin', 'admin', 'user', 'vendor_level1', 'vendor_level2'])
    expect(options.filter(option => !option.disabled).map(option => option.value)).toEqual(['super_admin', 'admin', 'user'])
  })

  it.each(['vendor_level1', 'vendor_level2'] as const)('displays and preserves %s when saving profile changes', async role => {
    const wrapper = mountModal(1, role)
    const select = wrapper.getComponent(Select)
    const options = select.props('options')
    expect(select.props('modelValue')).toBe(role)
    expect(options.find(option => option.value === role)?.label).toBe(`admin.users.roles.${role}`)
    expect(options.filter(option => !option.disabled).map(option => option.value)).toEqual([role])

    await wrapper.get('textarea').setValue('Updated supplier notes')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ role, notes: 'Updated supplier notes' }))
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('does not grant role changes to administrators without staff management', () => {
    auth.isSuperAdmin = false
    auth.can.mockReturnValue(false)
    const options = mountModal(1).getComponent(Select).props('options')
    expect(options.map(option => option.value)).toEqual(['user', 'vendor_level1', 'vendor_level2'])
    expect(options.filter(option => !option.disabled).map(option => option.value)).toEqual(['user'])
  })

  it('uses the same order for creation without offering standalone supplier identities', () => {
    const wrapper = mount(UserCreateModal, {
      props: { show: true },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true, TotpStepUpDialog: true } }
    })
    expect(wrapper.findAll('select option').map(option => option.attributes('value'))).toEqual(['super_admin', 'admin', 'user'])
  })
})

// These legacy functional cases exercise the migrated full administrator.
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
