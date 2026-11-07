import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/features/identity/pages/LoginView.vue'

const { loadPublicSettingsMock, pushMock, loginMock, showErrorMock } = vi.hoisted(() => ({
  loadPublicSettingsMock: vi.fn(),
  pushMock: vi.fn(),
  loginMock: vi.fn(),
  showErrorMock: vi.fn()
}))

const publicSettings = {
  registration_enabled: true,
  turnstile_enabled: false,
  turnstile_site_key: '',
  tencent_captcha_enabled: false,
  tencent_captcha_app_id: '',
  aliyun_captcha_enabled: false,
  aliyun_captcha_scene_id: '',
  aliyun_captcha_prefix: '',
  linuxdo_oauth_enabled: false,
  dingtalk_oauth_enabled: false,
  wechat_oauth_enabled: false,
  backend_mode_enabled: false,
  oidc_oauth_enabled: false,
  oidc_oauth_provider_name: 'OIDC',
  github_oauth_enabled: false,
  google_oauth_enabled: false,
  password_reset_enabled: false,
  passkey_enabled: false,
  login_agreement_enabled: false,
  login_agreement_documents: []
}

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushMock,
    currentRoute: { value: { query: {} } }
  })
}))

vi.mock('vue-i18n', async () => {
  const { default: zh } = await import('@/features/identity/locales/zh')
  const errors: Record<string, string> = zh.auth.errors
  const t = (key: string) => key.startsWith('auth.errors.')
    ? errors[key.slice('auth.errors.'.length)] ?? key
    : key
  return { createI18n: () => ({ global: { t } }), useI18n: () => ({ t }) }
})

vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => ({
    login: loginMock,
    loginWithPasskey: vi.fn(),
    login2FA: vi.fn()
  }) }))

vi.mock('@/features/identity/ports', async () => {
  const createNotifications = () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showWarning: vi.fn()
  })
  return {
    useIdentityNotifications: createNotifications,
    useIdentityAuthShell: () => ({ name: 'AuthLayout', template: '<div><slot /><slot name="footer" /></div>' }),
    useIdentityPortal: () => ({ isSupplierPortal: false, safePortalRedirect: () => '/vendor/dashboard' }),
    useIdentitySettings: () => {
      const fixture = createNotifications() as { cachedPublicSettings?: unknown; publicSettingsLoaded?: boolean; fetchPublicSettings?: (force?: boolean) => Promise<unknown> }
      return {
        get current() { return fixture.cachedPublicSettings ?? null },
        get loaded() { return fixture.publicSettingsLoaded ?? false },
        load: () => fixture.fetchPublicSettings ? fixture.fetchPublicSettings(true) : ((...args: unknown[]) => loadPublicSettingsMock(...args))()
      }
    }
  }
})

vi.mock('@/features/identity/api/auth', () => ({
  buildOAuthLoginStartURL: vi.fn(),
  
  isTotp2FARequired: vi.fn(() => false),
  isWeChatWebOAuthEnabled: vi.fn(() => false),
  startOAuthLogin: vi.fn()
}))

function mountLogin() {
  return mount(LoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        DingTalkOAuthSection: true,
        EmailOAuthButtons: true,
        Icon: true,
        LinuxDoOAuthSection: true,
        LoginAgreementPrompt: true,
        OidcOAuthSection: true,
        RouterLink: { template: '<a><slot /></a>' },
        TotpLoginModal: true,
        TurnstileWidget: true,
        WechatOAuthSection: true,
        transition: false
      }
    }
  })
}

describe('LoginView registration entry', () => {
  beforeEach(() => {
    loadPublicSettingsMock.mockReset()
    pushMock.mockReset()
    loginMock.mockReset()
    showErrorMock.mockReset()
    loadPublicSettingsMock.mockResolvedValue(publicSettings)
  })

  it.each([
    [{ status: 401, code: 401, reason: 'INVALID_CREDENTIALS', message: 'invalid email or password' }, '邮箱或密码错误'],
    [{ status: 403, code: 403, reason: 'USER_NOT_ACTIVE', message: 'account has been disabled' }, '账号已被禁用']
  ])('localizes a rejected login using the server reason and never navigates', async (error, expectedMessage) => {
    loginMock.mockRejectedValueOnce(error)
    const wrapper = mountLogin()
    await flushPromises()
    await wrapper.get('#email').setValue('rejected@fixture.invalid')
    await wrapper.get('#password').setValue('invalid-local-fixture')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(expectedMessage)
    expect(pushMock).not.toHaveBeenCalled()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('exposes the password toggle name and state without changing the value or submitting', async () => {
    const wrapper = mountLogin()
    await flushPromises()
    const password = wrapper.get<HTMLInputElement>('#password')
    await password.setValue('local-visibility-fixture')
    const toggle = wrapper.get('button[aria-label="auth.showPassword"]')

    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.attributes('aria-controls')).toBe('password')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    for (const visible of [true, false]) {
      await toggle.trigger('click')
      expect(toggle.attributes('aria-label')).toBe('auth.showPassword')
      expect(toggle.attributes('aria-pressed')).toBe(String(visible))
      expect(password.attributes('type')).toBe(visible ? 'text' : 'password')
      expect(password.element.value).toBe('local-visibility-fixture')
    }
    expect(loginMock).not.toHaveBeenCalled()
    expect(pushMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows the registration entry when registration is enabled', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.signUp')
  })

  it('hides the registration entry when registration is disabled', async () => {
    loadPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      registration_enabled: false
    })

    const wrapper = mountLogin()
    await flushPromises()

    expect(wrapper.text()).not.toContain('auth.signUp')
  })
})
