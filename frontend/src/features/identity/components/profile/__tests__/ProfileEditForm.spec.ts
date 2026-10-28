import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ProfileEditForm from '@/features/identity/components/profile/ProfileEditForm.vue'

const { updateProfileMock, showErrorMock, authState } = vi.hoisted(() => ({
  updateProfileMock: vi.fn(),
  showErrorMock: vi.fn(),
  authState: { user: { username: 'alice' } },
}))

vi.mock('@/features/identity/api/profile', () => ({ profileAPI: { updateProfile: updateProfileMock } }))
vi.mock('@/features/identity/model/auth', () => ({
  useAuthStore: () => authState,
}))
vi.mock('@/features/identity/ports', () => {
  const createFixture = () => ({ showError: showErrorMock })
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
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('ProfileEditForm', () => {
  it.each([
    [{ status: 400, code: 'VALIDATION_ERROR', message: 'username is too long' }, 'username is too long'],
    [{ response: { data: { detail: 'backend failure' } } }, 'backend failure'],
    [{}, 'profile.updateFailed'],
  ])('shows API failure %j without changing the saved profile', async (error, expectedMessage) => {
    updateProfileMock.mockRejectedValue(error)
    const wrapper = mount(ProfileEditForm, { props: { initialUsername: 'alice' } })

    await wrapper.get('#username').setValue('new-name')
    await wrapper.get('form').trigger('submit.prevent')

    expect(updateProfileMock).toHaveBeenCalledWith({ username: 'new-name' })
    expect(showErrorMock).toHaveBeenLastCalledWith(expectedMessage)
    expect(authState.user.username).toBe('alice')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
  })
})
