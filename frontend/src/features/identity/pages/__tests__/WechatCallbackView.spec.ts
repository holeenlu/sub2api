import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WechatCallbackView from '@/features/identity/pages/WechatCallbackView.vue'

const {
  exchangePendingOAuthCompletionMock,
  completeWeChatOAuthRegistrationMock,
  login2FAMock,
  apiClientPostMock,
  sendVerifyCodeMock,
  sendPendingOAuthVerifyCodeMock,
  loadPublicSettingsMock,
  prepareOAuthBindAccessTokenCookieMock,
  getAuthTokenMock,
  replaceMock,
  setTokenMock,
  setPendingAuthSessionMock,
  clearPendingAuthSessionMock,
  showSuccessMock,
  showErrorMock,
  fetchPublicSettingsMock,
  routeState,
  locationState,
  appStoreState,
} = vi.hoisted(() => ({
  exchangePendingOAuthCompletionMock: vi.fn(),
  completeWeChatOAuthRegistrationMock: vi.fn(),
  login2FAMock: vi.fn(),
  apiClientPostMock: vi.fn(),
  sendVerifyCodeMock: vi.fn(),
  sendPendingOAuthVerifyCodeMock: vi.fn(),
  loadPublicSettingsMock: vi.fn(),
  prepareOAuthBindAccessTokenCookieMock: vi.fn(),
  getAuthTokenMock: vi.fn(),
  replaceMock: vi.fn(),
  setTokenMock: vi.fn(),
  setPendingAuthSessionMock: vi.fn(),
  clearPendingAuthSessionMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  fetchPublicSettingsMock: vi.fn(),
  routeState: {
    query: {} as Record<string, unknown>,
  },
  locationState: {
    current: {
      href: 'http://localhost/auth/wechat/callback',
      hash: '',
      search: '',
      pathname: '/auth/wechat/callback'
    } as { href: string; hash: string; search: string; pathname: string },
  },
  appStoreState: {
    cachedPublicSettings: null as null | Record<string, unknown>,
    publicSettingsLoaded: false,
  },
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({
    replace: replaceMock,
  }),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
    },
  }),
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      if (key === 'auth.oauthFlow.totpHint') {
        return `verify ${params?.account ?? ''}`.trim()
      }
      if (key === 'auth.oidc.callbackTitle') {
        return `Signing you in with ${params?.providerName ?? ''}`.trim()
      }
      if (key === 'auth.oidc.callbackProcessing') {
        return `Completing login with ${params?.providerName ?? ''}`.trim()
      }
      if (key === 'auth.oidc.invitationRequired') {
        return `${params?.providerName ?? ''} invitation required`.trim()
      }
      if (key === 'auth.oidc.completeRegistration') {
        return 'Complete registration'
      }
      if (key === 'auth.oidc.completing') {
        return 'Completing'
      }
      if (key === 'auth.oidc.backToLogin') {
        return 'Back to login'
      }
      if (key === 'auth.invitationCodePlaceholder') {
        return 'Invitation code'
      }
      if (key === 'auth.loginSuccess') {
        return 'Login success'
      }
      if (key === 'auth.loginFailed') {
        return 'Login failed'
      }
      if (key === 'auth.oidc.callbackHint') {
        return 'Callback hint'
      }
      if (key === 'auth.oidc.callbackMissingToken') {
        return 'Missing login token'
      }
      if (key === 'auth.oidc.completeRegistrationFailed') {
        return 'Complete registration failed'
      }
      return key
    },
  }),
}))

vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => ({
    setToken: setTokenMock,
    setPendingAuthSession: setPendingAuthSessionMock,
    clearPendingAuthSession: clearPendingAuthSessionMock,
  }) }))

vi.mock('@/features/identity/ports', async () => {
  const createNotifications = () => ({
    ...appStoreState,
    showSuccess: showSuccessMock,
    showError: showErrorMock,
    fetchPublicSettings: fetchPublicSettingsMock,
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
        load: () => fixture.fetchPublicSettings ? fixture.fetchPublicSettings(true) : ((...args: any[]) => loadPublicSettingsMock(...args))()
      }
    }
  }
})

vi.mock('@/shared/http', () => ({
  apiClient: {
    post: (...args: any[]) => apiClientPostMock(...args),
  },
}))

vi.mock('@/features/identity/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/identity/api/auth')>('@/features/identity/api/auth')
  return {
    ...actual,
    exchangePendingOAuthCompletion: (...args: any[]) => exchangePendingOAuthCompletionMock(...args),
    completeWeChatOAuthRegistration: (...args: any[]) => completeWeChatOAuthRegistrationMock(...args),
    login2FA: (...args: any[]) => login2FAMock(...args),
    sendVerifyCode: (...args: any[]) => sendVerifyCodeMock(...args),
    sendPendingOAuthVerifyCode: (...args: any[]) => sendPendingOAuthVerifyCodeMock(...args),
    
    prepareOAuthBindAccessTokenCookie: (...args: any[]) => prepareOAuthBindAccessTokenCookieMock(...args),
    getAuthToken: (...args: any[]) => getAuthTokenMock(...args),
  }
})

describe('WechatCallbackView', () => {
  beforeEach(() => {
    exchangePendingOAuthCompletionMock.mockReset()
    completeWeChatOAuthRegistrationMock.mockReset()
    login2FAMock.mockReset()
    apiClientPostMock.mockReset()
    sendVerifyCodeMock.mockReset()
    sendPendingOAuthVerifyCodeMock.mockReset()
    loadPublicSettingsMock.mockReset()
    replaceMock.mockReset()
    setTokenMock.mockReset()
    setPendingAuthSessionMock.mockReset()
    clearPendingAuthSessionMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    prepareOAuthBindAccessTokenCookieMock.mockReset()
    getAuthTokenMock.mockReset()
    fetchPublicSettingsMock.mockReset()
    routeState.query = {}
    appStoreState.cachedPublicSettings = null
    appStoreState.publicSettingsLoaded = false
    localStorage.clear()
    sessionStorage.clear()
    locationState.current = {
      href: 'http://localhost/auth/wechat/callback',
      hash: '',
      search: '',
      pathname: '/auth/wechat/callback'
    }
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState.current,
    })
    Object.defineProperty(window.navigator, 'userAgent', {
      configurable: true,
      value: 'Mozilla/5.0',
    })
    loadPublicSettingsMock.mockResolvedValue({
      invitation_code_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: '',
    })
  })

  it('overrides an incompatible query mode with the configured open capability during bind recovery', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      mode: 'mp',
      redirect: '/profile',
    }
    appStoreState.cachedPublicSettings = {
      wechat_oauth_enabled: true,
      wechat_oauth_open_enabled: true,
      wechat_oauth_mp_enabled: false,
    }
    appStoreState.publicSettingsLoaded = true
    getAuthTokenMock.mockReturnValue('current-auth-token')

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(prepareOAuthBindAccessTokenCookieMock).toHaveBeenCalledTimes(1)
    expect(locationState.current.href).toContain('mode=open')
    expect(locationState.current.href).not.toContain('mode=mp')
  })

  it('falls back to the query mode when capability settings cannot be confirmed', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      mode: 'mp',
      redirect: '/profile',
    }
    fetchPublicSettingsMock.mockResolvedValue(null)
    getAuthTokenMock.mockReturnValue('current-auth-token')

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(prepareOAuthBindAccessTokenCookieMock).toHaveBeenCalledTimes(1)
    expect(locationState.current.href).toContain('mode=mp')
  })

  it('ignores legacy aggregate wechat settings and reuses the query mode during bind recovery', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      mode: 'open',
      redirect: '/profile',
    }
    appStoreState.cachedPublicSettings = {
      wechat_oauth_enabled: true,
    }
    appStoreState.publicSettingsLoaded = true
    getAuthTokenMock.mockReturnValue('current-auth-token')

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(prepareOAuthBindAccessTokenCookieMock).toHaveBeenCalledTimes(1)
    expect(locationState.current.href).toContain('mode=open')
  })

  it('accepts the legacy fragment token success callback without pending-session exchange', async () => {
    locationState.current.hash =
      '#access_token=legacy-access-token&refresh_token=legacy-refresh-token&expires_in=3600&token_type=Bearer&redirect=%2Flegacy-dashboard'
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState.current,
    })
    setTokenMock.mockResolvedValue({})

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).not.toHaveBeenCalled()
    expect(setTokenMock).toHaveBeenCalledWith('legacy-access-token')
    expect(localStorage.getItem('refresh_token')).toBe('legacy-refresh-token')
    expect(localStorage.getItem('token_expires_at')).not.toBeNull()
    expect(showSuccessMock).toHaveBeenCalledWith('Login success')
    expect(replaceMock).toHaveBeenCalledWith('/legacy-dashboard')
  })

  it('does not send adoption decisions during the initial exchange', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      expires_in: 3600,
      redirect: '/dashboard',
      adoption_required: true,
    })
    setTokenMock.mockResolvedValue({})

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenCalledWith()
    expect(exchangePendingOAuthCompletionMock).toHaveBeenCalledTimes(1)
  })

  it('waits for explicit adoption confirmation before finishing a non-invitation login', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/dashboard',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        access_token: 'wechat-access-token',
        refresh_token: 'wechat-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
        redirect: '/dashboard',
      })
    setTokenMock.mockResolvedValue({})

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('WeChat Nick')
    expect(setTokenMock).not.toHaveBeenCalled()
    expect(replaceMock).not.toHaveBeenCalled()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[1].setValue(false)

    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(1)
    await buttons[0].trigger('click')
    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(1)
    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(2, {
      adoptDisplayName: true,
      adoptAvatar: false,
    })
    expect(setTokenMock).toHaveBeenCalledWith('wechat-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/dashboard')
    expect(localStorage.getItem('refresh_token')).toBe('wechat-refresh-token')
  })

  it('supports bind completion after adoption confirmation', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/dashboard',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        redirect: '/profile/connections',
      })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(2, {
      adoptDisplayName: true,
      adoptAvatar: true,
    })
    expect(setTokenMock).not.toHaveBeenCalled()
    expect(clearPendingAuthSessionMock).toHaveBeenCalledTimes(1)
    expect(showSuccessMock).toHaveBeenCalledWith('profile.authBindings.bindSuccess')
    expect(replaceMock).toHaveBeenCalledWith('/profile/connections')
  })

  it('collects email, password, and verify code for pending oauth account creation and submits adoption decisions', async () => {
    loadPublicSettingsMock.mockResolvedValue({
      invitation_code_enabled: true,
      turnstile_enabled: false,
      turnstile_site_key: '',
    })
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        access_token: 'new-access-token',
        refresh_token: 'new-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
      },
    })
    setTokenMock.mockResolvedValue({})

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[1].setValue(false)
    await wrapper.get('[data-testid="wechat-create-account-email"]').setValue('  new@example.com  ')
    await wrapper.get('[data-testid="wechat-create-account-password"]').setValue('secret-123')
    await wrapper.get('[data-testid="wechat-create-account-verify-code"]').setValue('246810')
    await wrapper.get('[data-testid="wechat-create-account-submit"]').trigger('click')
    await flushPromises()

    expect(apiClientPostMock).toHaveBeenCalledWith('/auth/oauth/pending/create-account', {
      email: 'new@example.com',
      password: 'secret-123',
      verify_code: '246810',
      adopt_display_name: true,
      adopt_avatar: false,
    })
    expect(setTokenMock).toHaveBeenCalledWith('new-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/welcome')
  })

  it('persists a pending auth session when the oauth flow still needs account creation', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
    })

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(setPendingAuthSessionMock).toHaveBeenCalledWith({
      token: '',
      token_field: 'pending_oauth_token',
      provider: 'wechat',
      redirect: '/welcome',
    })
  })

  it('switches to bind-login when create-account returns EMAIL_EXISTS', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
    })
    apiClientPostMock.mockRejectedValue({
      response: {
        data: {
          reason: 'EMAIL_EXISTS',
          message: 'email already exists',
        },
      },
    })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()
    await wrapper.get('[data-testid="wechat-create-account-email"]').setValue('existing@example.com')
    await wrapper.get('[data-testid="wechat-create-account-password"]').setValue('secret-123')
    await wrapper.get('[data-testid="wechat-create-account-submit"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-testid="wechat-bind-login-email"]').element as HTMLInputElement).value).toBe(
      'existing@example.com'
    )
  })

  it('shows create-account failures through toast without inline error text', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
    })
    apiClientPostMock.mockRejectedValue(new Error('create failed'))

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()
    await wrapper.get('[data-testid="wechat-create-account-email"]').setValue('new@example.com')
    await wrapper.get('[data-testid="wechat-create-account-password"]').setValue('secret-123')
    await wrapper.get('[data-testid="wechat-create-account-submit"]').trigger('click')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith('create failed')
    expect(wrapper.text()).not.toContain('create failed')
  })

  it('sends a verify code for pending oauth account creation', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
    })
    sendPendingOAuthVerifyCodeMock.mockResolvedValue({
      message: 'sent',
      countdown: 60,
    })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    await wrapper.get('[data-testid="wechat-create-account-email"]').setValue('  new@example.com  ')
    await wrapper.get('[data-testid="wechat-create-account-send-code"]').trigger('click')
    await flushPromises()

    expect(sendPendingOAuthVerifyCodeMock).toHaveBeenCalledWith({
      email: 'new@example.com',
    })
  })

  it('shows bind-login form for existing account binding and submits credentials with adoption decisions', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/profile/security',
      email: 'existing@example.com',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        access_token: 'bind-access-token',
        refresh_token: 'bind-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
      },
    })
    setTokenMock.mockResolvedValue({})

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[0].setValue(false)
    await wrapper.get('[data-testid="wechat-bind-login-email"]').setValue('existing@example.com')
    await wrapper.get('[data-testid="wechat-bind-login-password"]').setValue('secret-password')
    await wrapper.get('[data-testid="wechat-bind-login-submit"]').trigger('click')
    await flushPromises()

    expect(apiClientPostMock).toHaveBeenCalledWith('/auth/oauth/pending/bind-login', {
      email: 'existing@example.com',
      password: 'secret-password',
      adopt_display_name: false,
      adopt_avatar: true,
    })
    expect(setTokenMock).toHaveBeenCalledWith('bind-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/profile/security')
  })

  it('allows switching from server-driven bind-login to create-account mode', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/welcome',
      email: 'existing@example.com',
    })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    await wrapper.get('button.btn-secondary').trigger('click')
    await flushPromises()

    const createAccountEmail = wrapper.get('[data-testid="wechat-create-account-email"]')
    expect((createAccountEmail.element as HTMLInputElement).value).toBe('existing@example.com')
  })

  it('reuses query email for bind-login when backend does not echo it back', async () => {
    routeState.query = {
      email: 'resume@example.com',
    }
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/profile',
    })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    const bindEmail = wrapper.get('[data-testid="wechat-bind-login-email"]')
    expect((bindEmail.element as HTMLInputElement).value).toBe('resume@example.com')
  })

  it('keeps rendering pending bind-login UI when adoption confirmation leads to another pending step', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/profile',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        step: 'bind_login_required',
        redirect: '/profile',
        email: 'existing@example.com',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()

    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(replaceMock).not.toHaveBeenCalled()
    expect((wrapper.get('[data-testid="wechat-bind-login-email"]').element as HTMLInputElement).value).toBe(
      'existing@example.com'
    )
  })

  it('handles bind-login 2FA challenge before redirecting', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'adopt_existing_user_by_email',
      redirect: '/profile',
      email: 'existing@example.com',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        requires_2fa: true,
        temp_token: 'temp-123',
        user_email_masked: 'o***g@example.com',
      },
    })
    login2FAMock.mockResolvedValue({
      access_token: '2fa-access-token',
      refresh_token: '2fa-refresh-token',
      expires_in: 3600,
    })
    setTokenMock.mockResolvedValue({})

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    await wrapper.get('[data-testid="wechat-bind-login-password"]').setValue('secret-password')
    await wrapper.get('[data-testid="wechat-bind-login-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('o***g@example.com')
    expect(login2FAMock).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="wechat-bind-login-totp"]').setValue('123456')
    await wrapper.get('[data-testid="wechat-bind-login-totp-submit"]').trigger('click')
    await flushPromises()

    expect(login2FAMock).toHaveBeenCalledWith({
      temp_token: 'temp-123',
      totp_code: '123456',
    })
    expect(setTokenMock).toHaveBeenCalledWith('2fa-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/profile')
    expect(localStorage.getItem('refresh_token')).toBe('2fa-refresh-token')
  })

  it('restarts the current-user bind flow after returning from login', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      redirect: '/profile',
      mode: 'mp',
    }
    getAuthTokenMock.mockReturnValue('existing-auth-token')

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).not.toHaveBeenCalled()
    expect(prepareOAuthBindAccessTokenCookieMock).toHaveBeenCalledTimes(1)
    expect(locationState.current.href).toContain('/api/v1/auth/oauth/wechat/bind/start?')
    expect(locationState.current.href).toContain('mode=mp')
    expect(locationState.current.href).toContain('intent=bind_current_user')
    expect(locationState.current.href).toContain('redirect=%2Fprofile')
  })

  it('redirects back to login instead of falling through when bind-existing resume has no auth token', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      redirect: '/profile',
      mode: 'mp',
      email: 'resume@example.com',
    }
    getAuthTokenMock.mockReturnValue(null)

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).not.toHaveBeenCalled()
    expect(replaceMock).toHaveBeenCalledTimes(1)
    expect(replaceMock.mock.calls[0]?.[0]).toContain('/login?')
    expect(replaceMock.mock.calls[0]?.[0]).toContain('wechat_bind_existing%3D1')
    expect(replaceMock.mock.calls[0]?.[0]).toContain('mode%3Dmp')
    expect(replaceMock.mock.calls[0]?.[0]).toContain('email=resume%40example.com')
  })
})
