import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ResetPasswordView from '../ResetPasswordView.vue'

const { resetPasswordMock } = vi.hoisted(() => ({ resetPasswordMock: vi.fn() }))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: { email: 'reset@fixture.invalid', token: 'local-reset-fixture' } })
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/features/identity/api/auth', () => ({ resetPassword: resetPasswordMock }))
vi.mock('@/features/identity/ports', () => ({
  useIdentityAuthShell: () => ({ template: '<div><slot /><slot name="footer" /></div>' }),
  useIdentityNotifications: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))

describe('ResetPasswordView password visibility', () => {
  beforeEach(() => resetPasswordMock.mockReset())

  it.each([
    ['password', 'auth.showPassword', 'confirmPassword'],
    ['confirmPassword', 'auth.showConfirmPassword', 'password']
  ])('names the %s toggle without submitting or exposing the other password', async (id, label, otherId) => {
    const wrapper = mount(ResetPasswordView, {
      global: { stubs: { Icon: true, RouterLink: true } }
    })
    await flushPromises()
    const password = wrapper.get<HTMLInputElement>(`#${id}`)
    await password.setValue('local-visibility-fixture')
    const toggle = wrapper.get(`button[aria-label="${label}"]`)

    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.attributes('aria-controls')).toBe(id)
    expect(toggle.attributes('aria-pressed')).toBe('false')
    for (const visible of [true, false]) {
      await toggle.trigger('click')
      expect(toggle.attributes('aria-label')).toBe(label)
      expect(toggle.attributes('aria-pressed')).toBe(String(visible))
      expect(password.attributes('type')).toBe(visible ? 'text' : 'password')
      expect(password.element.value).toBe('local-visibility-fixture')
      expect(wrapper.get(`#${otherId}`).attributes('type')).toBe('password')
    }
    expect(resetPasswordMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
