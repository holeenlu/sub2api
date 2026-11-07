import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AdminComplianceDialog from '../AdminComplianceDialog.vue'
import { useAdminComplianceStore } from '../../model/adminCompliance'
import { useAuthStore } from '../../model/auth'
import { identityNotificationsKey } from '../../ports'
import type { AdminComplianceStatus } from '../../api/compliance'
import type { User } from '../../api/profileTypes'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
const notifications = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() }))
vi.mock('@/features/identity/api/compliance', () => ({ default: api }))
const authRequests = vi.hoisted(() => ({ getCurrentUser: vi.fn() }))
vi.mock('@/features/identity/api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/identity/api/auth')>()
  return { ...actual, authAPI: { ...actual.authAPI, getCurrentUser: authRequests.getCurrentUser } }
})
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let pinia: ReturnType<typeof createPinia>
enableAutoUnmount(afterEach)
beforeEach(async () => {
  vi.resetAllMocks()
  localStorage.clear()
  authRequests.getCurrentUser.mockResolvedValue({ data: { id: 7, role: 'admin' } })
  pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = { id: 7, role: 'admin' } as User
  await auth.setToken('first-session')
})
afterEach(() => disposePinia(pinia))
function status(required = true): AdminComplianceStatus {
  return { required, version: 'test-terms', document_path_en: '', document_path_zh: '', document_url_en: '', document_url_zh: '', ack_phrase_en: 'Exact manual phrase', ack_phrase_zh: '测试短语' }
}
function open() {
  useAdminComplianceStore().requireAcknowledgement(status())
  return mount(AdminComplianceDialog, { global: {
    plugins: [pinia], provide: { [identityNotificationsKey as symbol]: notifications },
    stubs: {
      BaseDialog: { name: 'BaseDialog', props: ['show', 'closeOnEscape', 'closeOnClickOutside', 'showCloseButton'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Input: { props: ['modelValue', 'disabled', 'error'], emits: ['update:modelValue', 'enter'], template: '<div><input :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" /><p>{{ error }}</p></div>' },
      Icon: true,
    },
  } })
}
function deferred() {
  let resolve!: (value: AdminComplianceStatus) => void
  let reject!: (error: Error) => void
  const promise = new Promise<AdminComplianceStatus>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('explicit administrator commitment dialog', () => {
  it('does not auto-accept or allow closing to bypass manual confirmation', async () => {
    const wrapper = open()
    await flushPromises()
    expect(api.accept).not.toHaveBeenCalled()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    const dialog = wrapper.getComponent({ name: 'BaseDialog' })
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.props('closeOnClickOutside')).toBe(false)
    expect(dialog.props('showCloseButton')).toBe(false)
    dialog.vm.$emit('close')
    expect(useAdminComplianceStore().shouldShow).toBe(true)
    await wrapper.get('input').setValue('incorrect phrase')
    await wrapper.get('input').trigger('keydown.enter')
    expect(api.accept).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('adminCompliance.inputMismatch')
  })

  it('submits only an exact manually entered phrase and waits for server acceptance', async () => {
    const pending = deferred()
    api.accept.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    await wrapper.get('input').setValue('Exact manual phrase')
    await wrapper.get('button.btn-primary').trigger('click')
    expect(api.accept).toHaveBeenCalledWith({ phrase: 'Exact manual phrase', language: 'en' })
    expect(useAdminComplianceStore().required).toBe(true)
    expect(notifications.showSuccess).not.toHaveBeenCalled()
    await wrapper.get('input').trigger('keydown.enter')
    expect(api.accept).toHaveBeenCalledOnce()
    pending.resolve(status(false))
    await flushPromises()
    expect(useAdminComplianceStore().required).toBe(false)
    expect(notifications.showSuccess).toHaveBeenCalledWith('adminCompliance.accepted')
  })

  it('keeps the gate open on a current server rejection', async () => {
    api.accept.mockRejectedValueOnce(new Error('Server rejected confirmation'))
    const wrapper = open()
    await wrapper.get('input').setValue('Exact manual phrase')
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(useAdminComplianceStore().required).toBe(true)
    expect(notifications.showError).toHaveBeenCalledWith('Server rejected confirmation')
    expect(notifications.showSuccess).not.toHaveBeenCalled()
  })

  it.each(['resolve', 'reject'] as const)('ignores old confirmation %s after the same user starts a new session', async outcome => {
    const pending = deferred()
    api.accept.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    await wrapper.get('input').setValue('Exact manual phrase')
    await wrapper.get('button.btn-primary').trigger('click')
    await useAuthStore().setToken('second-session')
    useAdminComplianceStore().requireAcknowledgement(status())
    await flushPromises()
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    if (outcome === 'resolve') pending.resolve(status(false))
    else pending.reject(new Error('Old error'))
    await flushPromises()
    expect(useAdminComplianceStore().required).toBe(true)
    expect(notifications.showSuccess).not.toHaveBeenCalled()
    expect(notifications.showError).not.toHaveBeenCalled()
  })

  it('clears typed acceptance when the server changes the terms version', async () => {
    const wrapper = open()
    await wrapper.get('input').setValue('Exact manual phrase')
    useAdminComplianceStore().requireAcknowledgement({ ...status(), version: 'updated-terms' })
    await flushPromises()
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
    expect(api.accept).not.toHaveBeenCalled()
  })
})
