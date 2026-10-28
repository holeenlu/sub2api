import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import ProfilePasswordForm from '@/features/identity/components/profile/ProfilePasswordForm.vue'

const auth = vi.hoisted(() => ({ current: { identityScopeKey: 'session-a', isAuthenticated: true } }))
vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => auth.current }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  auth.current = reactive({ identityScopeKey: 'session-a', isAuthenticated: true })
})

const { changePasswordMock, showSuccessMock, showErrorMock } = vi.hoisted(() => ({
  changePasswordMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/features/identity/api/profile', () => ({ profileAPI: {
    changePassword: changePasswordMock
  } }))

vi.mock('@/features/identity/ports', () => {
  const createFixture = () => ({
    showSuccess: showSuccessMock,
    showError: showErrorMock
  })
  return {
    useIdentityNotifications: createFixture,
    useIdentitySettings: () => {
      const fixture = createFixture() as { cachedPublicSettings?: unknown; publicSettingsLoaded?: boolean; fetchPublicSettings?: () => Promise<unknown> }
      return {
        get current() { return fixture.cachedPublicSettings ?? null },
        get loaded() { return fixture.publicSettingsLoaded ?? false },
        load: () => fixture.fetchPublicSettings ? fixture.fetchPublicSettings() : Promise.resolve(fixture.cachedPublicSettings)
      }
    }
  }
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const translations: Record<string, string> = {
          'profile.changePassword': 'Change Password',
          'profile.currentPassword': 'Current Password',
          'profile.newPassword': 'New Password',
          'profile.confirmNewPassword': 'Confirm New Password',
          'profile.passwordHint': 'Password must be at least 8 characters long',
          'profile.changingPassword': 'Changing...',
          'profile.changePasswordButton': 'Change Password',
          'profile.passwordsNotMatch': 'New passwords do not match',
          'profile.passwordTooShort': 'Password must be at least 8 characters long',
          'profile.passwordChangeSuccess': 'Password changed successfully',
          'profile.passwordChangeFailed': 'Failed to change password'
        }
        return translations[key] ?? key
      }
    })
  }
})

describe('ProfilePasswordForm', () => {
  it('shows validation failures as toast messages instead of inline errors', async () => {
    const wrapper = mount(ProfilePasswordForm)

    await wrapper.get('#old_password').setValue('old-password')
    await wrapper.get('#new_password').setValue('new-password')
    await wrapper.get('#confirm_password').setValue('different-password')
    await wrapper.get('form').trigger('submit.prevent')

    expect(changePasswordMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('New passwords do not match')
    expect(wrapper.find('.input-error-text').exists()).toBe(false)
  })

  it.each([
    [{ status: 400, code: 'PASSWORD_INCORRECT', message: 'current password is incorrect' }, 'current password is incorrect'],
    [{ response: { data: { detail: 'backend failure' } } }, 'backend failure'],
    [{}, 'Failed to change password'],
  ])('shows API failure %j as a toast', async (error, expectedMessage) => {
    changePasswordMock.mockRejectedValue(error)

    const wrapper = mount(ProfilePasswordForm)

    await wrapper.get('#old_password').setValue('old-password')
    await wrapper.get('#new_password').setValue('new-password')
    await wrapper.get('#confirm_password').setValue('new-password')
    await wrapper.get('form').trigger('submit.prevent')

    expect(changePasswordMock).toHaveBeenCalledWith('old-password', 'new-password', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(showErrorMock).toHaveBeenLastCalledWith(expectedMessage)
    expect(wrapper.find('.input-error-text').exists()).toBe(false)
  })

  it.each(['session', 'logout', 'unmount'])('clears sensitive state and ignores late success after %s', async transition => {
    let finish!: (value: { message: string }) => void
    changePasswordMock.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(ProfilePasswordForm)
    await wrapper.get('#old_password').setValue('old-password')
    await wrapper.get('#new_password').setValue('new-password')
    await wrapper.get('#confirm_password').setValue('new-password')
    await wrapper.get('form').trigger('submit.prevent')
    await wrapper.get('form').trigger('submit.prevent')
    expect(changePasswordMock).toHaveBeenCalledOnce()
    const signal = changePasswordMock.mock.calls[0][2].signal as AbortSignal
    if (transition === 'session') auth.current.identityScopeKey = 'session-b'
    else if (transition === 'logout') auth.current.isAuthenticated = false
    else wrapper.unmount()
    await flushPromises()
    expect(signal.aborted).toBe(true)
    if (transition !== 'unmount') {
      expect(wrapper.findAll('input').map(input => input.element.value)).toEqual(['', '', ''])
      await wrapper.get('#old_password').setValue('new-session-draft')
    }
    finish({ message: 'changed' })
    await flushPromises()
    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(showErrorMock).not.toHaveBeenCalled()
    if (transition !== 'unmount') expect(wrapper.get('#old_password').element.value).toBe('new-session-draft')
  })

  it('silences a rejected password request after unmount', async () => {
    let reject!: (error: Error) => void
    changePasswordMock.mockReturnValue(new Promise((_, no) => { reject = no }))
    const wrapper = mount(ProfilePasswordForm)
    await wrapper.get('#old_password').setValue('old-password')
    await wrapper.get('#new_password').setValue('new-password')
    await wrapper.get('#confirm_password').setValue('new-password')
    await wrapper.get('form').trigger('submit.prevent')
    wrapper.unmount()
    reject(new Error('late failure'))
    await flushPromises()
    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(showErrorMock).not.toHaveBeenCalled()
  })
})
