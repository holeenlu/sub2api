import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/features/identity/pages/LoginView.vue'

const loginMock = vi.fn()
const loginWithPasskeyMock = vi.fn()
const loadPublicSettingsMock = vi.fn()
const startOAuthLoginMock = vi.fn()
const verifyActionMock = vi.fn()
const captchaResetMock = vi.fn()
const locationState = { href: 'http://localhost/login' }

vi.mock('vue-router', () => ({
  useRouter: () => ({
    currentRoute: { value: { query: {} } },
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => ({
    login: (...args: unknown[]) => loginMock(...args),
    loginWithPasskey: (...args: unknown[]) => loginWithPasskeyMock(...args)
  }) }))

vi.mock('@/features/identity/ports', async () => {
  const createNotifications = () => ({
    showError: vi.fn(),
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

vi.mock('@/features/identity/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/identity/api/auth')>('@/features/identity/api/auth')
  return {
    ...actual,
    
    startOAuthLogin: (...args: unknown[]) => startOAuthLoginMock(...args),
    isTotp2FARequired: () => false,
    isWeChatWebOAuthEnabled: () => false
  }
})

const CaptchaChallengeStub = defineComponent({
  setup(_, { expose }) {
    expose({
      verifyAction: verifyActionMock,
      reset: captchaResetMock
    })
    return () => h('div')
  }
})

const OAuthButtonStub = defineComponent({
  emits: ['start'],
  setup(_, { emit }) {
    return () => h('button', {
      type: 'button',
      'data-testid': 'oauth-start',
      onClick: () => emit('start', {
        provider: 'github',
        params: { redirect: '/dashboard' }
      })
    })
  }
})

function mountLogin() {
  return mount(LoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        RouterLink: true,
        TurnstileWidget: CaptchaChallengeStub,
        Icon: true,
        LoginAgreementPrompt: true,
        TotpLoginModal: true,
        EmailOAuthButtons: OAuthButtonStub,
        LinuxDoOAuthSection: true,
        DingTalkOAuthSection: true,
        OidcOAuthSection: true,
        WechatOAuthSection: true
      }
    }
  })
}

describe('Tencent captcha action gate', () => {
  beforeEach(() => {
    loginMock.mockReset()
    loginWithPasskeyMock.mockReset()
    loadPublicSettingsMock.mockReset()
    startOAuthLoginMock.mockReset()
    verifyActionMock.mockReset()
    captchaResetMock.mockReset()
    loadPublicSettingsMock.mockResolvedValue({
      turnstile_enabled: false,
      turnstile_site_key: '',
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'tencent-app-id',
      backend_mode_enabled: false,
      password_reset_enabled: false,
      passkey_enabled: true,
      github_oauth_enabled: true,
      google_oauth_enabled: false
    })
    loginMock.mockResolvedValue({})
    loginWithPasskeyMock.mockResolvedValue({})
    startOAuthLoginMock.mockResolvedValue({ authorize_url: 'https://github.example/authorize' })
    verifyActionMock.mockResolvedValue({ token: 'ticket-1', randstr: '@rand-1' })
    Object.defineProperty(window, 'PublicKeyCredential', {
      configurable: true,
      value: class PublicKeyCredential {}
    })
    locationState.href = 'http://localhost/login'
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState
    })
  })

  it('clicking login opens Tencent captcha before calling login', async () => {
    const wrapper = mountLogin()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(loginMock).toHaveBeenCalledWith(expect.objectContaining({
      tencent_captcha_ticket: 'ticket-1',
      tencent_captcha_randstr: '@rand-1'
    }))
  })

  it('does not call login when Tencent captcha is closed', async () => {
    verifyActionMock.mockResolvedValue(null)
    const wrapper = mountLogin()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(loginMock).not.toHaveBeenCalled()
  })

  it('does not open Tencent captcha when login form validation fails', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(verifyActionMock).not.toHaveBeenCalled()
    expect(loginMock).not.toHaveBeenCalled()
  })

  it('starts OAuth through the Tencent gate before navigating', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    await wrapper.get('[data-testid="oauth-start"]').trigger('click')
    await flushPromises()

    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(startOAuthLoginMock).toHaveBeenCalledWith(
      { provider: 'github', params: { redirect: '/dashboard' } },
      {
        tencent_captcha_ticket: 'ticket-1',
        tencent_captcha_randstr: '@rand-1'
      }
    )
    expect(locationState.href).toBe('https://github.example/authorize')
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })

  it('does not start OAuth when Tencent captcha is closed', async () => {
    verifyActionMock.mockResolvedValue(null)
    const wrapper = mountLogin()
    await flushPromises()

    await wrapper.get('[data-testid="oauth-start"]').trigger('click')
    await flushPromises()

    expect(startOAuthLoginMock).not.toHaveBeenCalled()
    expect(locationState.href).toBe('http://localhost/login')
  })

  it('passes a fresh Tencent proof to Passkey login', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    await wrapper.get('button.btn-secondary.w-full').trigger('click')
    await flushPromises()

    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(loginWithPasskeyMock).toHaveBeenCalledWith({
      tencent_captcha_ticket: 'ticket-1',
      tencent_captcha_randstr: '@rand-1'
    })
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })

  it('does not invoke Passkey when Tencent captcha is closed', async () => {
    verifyActionMock.mockResolvedValue(null)
    const wrapper = mountLogin()
    await flushPromises()

    await wrapper.get('button.btn-secondary.w-full').trigger('click')
    await flushPromises()

    expect(loginWithPasskeyMock).not.toHaveBeenCalled()
  })
})
