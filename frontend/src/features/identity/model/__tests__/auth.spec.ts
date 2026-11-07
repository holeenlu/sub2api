import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '@/features/identity/model/auth'
import { CanceledError } from 'axios'

// Mock authAPI
const mockLogin = vi.fn()
const mockLogin2FA = vi.fn()
const mockLogout = vi.fn()
const mockGetCurrentUser = vi.fn()
const mockRegister = vi.fn()
const mockRefreshToken = vi.fn()

vi.mock('@/features/identity/api/auth', () => ({
  authAPI: {
    login: (...args: any[]) => mockLogin(...args),
    login2FA: (...args: any[]) => mockLogin2FA(...args),
    logout: (...args: any[]) => mockLogout(...args),
    getCurrentUser: (...args: any[]) => mockGetCurrentUser(...args),
    register: (...args: any[]) => mockRegister(...args),
    refreshToken: (...args: any[]) => mockRefreshToken(...args),
  },
  isTotp2FARequired: (response: any) => response?.requires_2fa === true,
}))

const fakeUser = {
  id: 1,
  username: 'testuser',
  email: 'test@example.com',
  role: 'user' as const,
  balance: 100,
  concurrency: 5,
  status: 'active' as const,
  allowed_groups: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

const fakeAdminUser = {
  ...fakeUser,
  id: 2,
  username: 'admin',
  email: 'admin@example.com',
  role: 'admin' as const,
}

const fakeAuthResponse = {
  access_token: 'test-token-123',
  refresh_token: 'refresh-token-456',
  expires_in: 3600,
  token_type: 'Bearer',
  user: { ...fakeUser },
}

describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each(['login', 'logout', 'refreshUser'])('旧 %s 的完成不清除另一登录会话', async operation => {
    const store = useAuthStore()
    mockLogin.mockResolvedValueOnce(fakeAuthResponse)
    await store.login({ email: 'test@example.com', password: 'secret' })
    let finish!: () => void
    const delayed = new Promise((resolve, reject) => {
      finish = () => operation === 'logout' ? resolve(undefined) : reject(new CanceledError('old session'))
    })
    if (operation === 'login') mockLogin.mockReturnValueOnce(delayed)
    if (operation === 'logout') mockLogout.mockReturnValueOnce(delayed)
    if (operation === 'refreshUser') mockGetCurrentUser.mockReturnValueOnce(delayed)
    const pending = operation === 'login' ? store.login({ email: 'test@example.com', password: 'secret' })
      : operation === 'logout' ? store.logout() : store.refreshUser()
    const settled = operation === 'logout' ? expect(pending).resolves.toBeUndefined() : expect(pending).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, access_token: 'replacement-login', refresh_token: 'replacement-refresh', user: fakeAdminUser })
    await store.login({ email: 'admin@example.com', password: 'secret' })
    finish()
    await settled
    expect(store.token).toBe('replacement-login')
    expect(store.user?.id).toBe(2)
    expect(localStorage.getItem('auth_token')).toBe('replacement-login')
    expect(localStorage.getItem('refresh_token')).toBe('replacement-refresh')
  })

  it.each(['logout', 'refreshUser'])('%s 的 401 已清存储时仍清除原会话内存与定时器', async operation => {
    const store = useAuthStore()
    mockLogin.mockResolvedValueOnce(fakeAuthResponse)
    await store.login({ email: 'test@example.com', password: 'secret' })
    const expired = async () => {
      localStorage.clear()
      throw { status: 401 }
    }
    if (operation === 'logout') mockLogout.mockImplementationOnce(expired)
    else mockGetCurrentUser.mockImplementationOnce(expired)
    if (operation === 'logout') await store.logout()
    else await expect(store.refreshUser()).rejects.toMatchObject({ status: 401 })
    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    const userRequests = mockGetCurrentUser.mock.calls.length
    const tokenRequests = mockRefreshToken.mock.calls.length
    await vi.advanceTimersByTimeAsync(3600_000)
    expect(mockGetCurrentUser).toHaveBeenCalledTimes(userRequests)
    expect(mockRefreshToken).toHaveBeenCalledTimes(tokenRequests)
  })

  it('存储已清空时迟到注销不清除另一会话的内存', async () => {
    const store = useAuthStore()
    mockLogin.mockResolvedValueOnce(fakeAuthResponse)
    await store.login({ email: 'test@example.com', password: 'secret' })
    let finish!: () => void
    mockLogout.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }))
    const pending = store.logout()
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, access_token: 'replacement-login', user: fakeAdminUser })
    await store.login({ email: 'admin@example.com', password: 'secret' })
    localStorage.clear()
    finish()
    await pending
    expect(store.token).toBe('replacement-login')
    expect(store.user?.id).toBe(2)
  })

  // --- login ---

  describe('login', () => {
    it('成功登录后设置 token 和 user', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
      expect(localStorage.getItem('auth_token')).toBe('test-token-123')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(fakeUser))
    })

    it('登录失败时清除状态并抛出错误', async () => {
      mockLogin.mockRejectedValue(new Error('Invalid credentials'))
      const store = useAuthStore()

      await expect(store.login({ email: 'test@example.com', password: 'wrong' })).rejects.toThrow(
        'Invalid credentials'
      )

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('需要 2FA 时返回响应但不设置认证状态', async () => {
      const twoFAResponse = { requires_2fa: true, temp_token: 'temp-123' }
      mockLogin.mockResolvedValue(twoFAResponse)
      const store = useAuthStore()

      const result = await store.login({ email: 'test@example.com', password: '123456' })

      expect(result).toEqual(twoFAResponse)
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })
  })

  // --- login2FA ---

  describe('login2FA', () => {
    it('2FA 验证成功后设置认证状态', async () => {
      mockLogin2FA.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      const user = await store.login2FA('temp-123', '654321')

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(user).toEqual(fakeUser)
      expect(mockLogin2FA).toHaveBeenCalledWith({
        temp_token: 'temp-123',
        totp_code: '654321',
      })
    })

    it('2FA 验证失败时清除状态并抛出错误', async () => {
      mockLogin2FA.mockRejectedValue(new Error('Invalid TOTP'))
      const store = useAuthStore()

      await expect(store.login2FA('temp-123', '000000')).rejects.toThrow('Invalid TOTP')
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })
  })

  // --- logout ---

  describe('logout', () => {
    it('注销后清除所有状态和 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockLogout.mockResolvedValue(undefined)
      const store = useAuthStore()

      // 先登录
      await store.login({ email: 'test@example.com', password: '123456' })
      expect(store.isAuthenticated).toBe(true)

      // 注销
      await store.logout()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(localStorage.getItem('token_expires_at')).toBeNull()
    })
  })

  // --- checkAuth ---

  describe('checkAuth', () => {
    it('从 localStorage 恢复持久化状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))

      // Mock refreshUser (getCurrentUser) 防止后台刷新报错
      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBe('saved-token')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
    })

    it('localStorage 无数据时保持未认证状态', () => {
      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('localStorage 中用户数据损坏时清除状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', 'invalid-json{{{')

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(localStorage.getItem('auth_token')).toBeNull()
    })

    it('恢复 refresh token 和过期时间', () => {
      const futureTs = String(Date.now() + 3600_000)
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))
      localStorage.setItem('refresh_token', 'saved-refresh')
      localStorage.setItem('token_expires_at', futureTs)

      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.isAuthenticated).toBe(true)
    })

    it('恢复持久化 pending auth session', () => {
      localStorage.setItem(
        'pending_auth_session',
        JSON.stringify({
          token: 'pending-token',
          token_field: 'pending_auth_token',
          provider: 'wechat',
          redirect: '/profile',
        })
      )

      const store = useAuthStore()
      store.checkAuth()

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })
    })
  })

  describe('pending auth session', () => {
    it('persists and clears pending auth session state', () => {
      const store = useAuthStore()

      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      expect(store.hasPendingAuthSession).toBe(true)
      expect(JSON.parse(localStorage.getItem('pending_auth_session') || 'null')).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      store.clearPendingAuthSession()

      expect(store.hasPendingAuthSession).toBe(false)
      expect(localStorage.getItem('pending_auth_session')).toBeNull()
    })

    it('restores a persisted pending oauth session without requiring a token value', () => {
      const firstStore = useAuthStore()

      firstStore.setPendingAuthSession({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick'
      })

      setActivePinia(createPinia())
      const restoredStore = useAuthStore()
      restoredStore.checkAuth()

      expect(restoredStore.isAuthenticated).toBe(false)
      expect(restoredStore.hasPendingAuthSession).toBe(true)
      expect(restoredStore.pendingAuthSession).toEqual({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick',
        suggested_avatar_url: undefined
      })
    })

    it('preserves pending auth session when registration fails', async () => {
      const store = useAuthStore()
      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
      mockRegister.mockRejectedValue(new Error('Register failed'))

      await expect(
        store.register({ email: 'user@example.com', password: 'secret-123' })
      ).rejects.toThrow('Register failed')

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
    })
  })

  // --- isAdmin ---

  describe('isAdmin', () => {
    it('管理员用户返回 true', async () => {
      const adminResponse = { ...fakeAuthResponse, user: { ...fakeAdminUser } }
      mockLogin.mockResolvedValue(adminResponse)
      const store = useAuthStore()

      await store.login({ email: 'admin@example.com', password: '123456' })

      expect(store.isAdmin).toBe(true)
    })

    it('普通用户返回 false', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isAdmin).toBe(false)
    })

    it('未登录时返回 false', () => {
      const store = useAuthStore()
      expect(store.isAdmin).toBe(false)
    })
  })

  // --- refreshUser ---

  describe('refreshUser', () => {
    it('刷新用户数据并更新 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const updatedUser = { ...fakeUser, username: 'updated-name' }
      mockGetCurrentUser.mockResolvedValue({ data: updatedUser })

      const result = await store.refreshUser()

      expect(result).toEqual(updatedUser)
      expect(store.user).toEqual(updatedUser)
      expect(JSON.parse(localStorage.getItem('auth_user')!)).toEqual(updatedUser)
    })

    it('未认证时抛出错误', async () => {
      const store = useAuthStore()
      await expect(store.refreshUser()).rejects.toThrow('Not authenticated')
    })
  })

  // --- isSimpleMode ---

  describe('isSimpleMode', () => {
    it('run_mode 为 simple 时返回 true', async () => {
      const simpleResponse = {
        ...fakeAuthResponse,
        user: { ...fakeUser, run_mode: 'simple' as const },
      }
      mockLogin.mockResolvedValue(simpleResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isSimpleMode).toBe(true)
    })

    it('默认为 standard 模式', () => {
      const store = useAuthStore()
      expect(store.isSimpleMode).toBe(false)
    })
  })
})


describe('identity session ownership', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.useFakeTimers()
    vi.clearAllMocks()
    setActivePinia(createPinia())
    mockGetCurrentUser.mockResolvedValue({ data: fakeUser })
  })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

  function tokenFor(sid: string, jti: string): string {
    return 'header.' + btoa(JSON.stringify({ user_id: 1, role: 'user', sid, jti })) + '.signature'
  }

  it('keeps scope across token rotation but advances for a new same-user session', async () => {
    const auth = useAuthStore()
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, access_token: tokenFor('first', 'one') })
    await auth.login({ email: fakeUser.email, password: 'secret' })
    const originalScope = auth.identityScopeKey
    const originalEpoch = auth.sessionEpoch
    const rotated = tokenFor('first', 'two')
    localStorage.setItem('auth_token', rotated)
    auth.checkAuth()
    await Promise.resolve()
    expect(auth.sessionEpoch).toBe(originalEpoch)
    expect(auth.identityScopeKey).toBe(originalScope)
    expect(auth.token).toBe(rotated)
    expect(auth.identityScopeKey).not.toContain(rotated)
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, access_token: tokenFor('second', 'three') })
    await auth.login({ email: fakeUser.email, password: 'secret' })
    expect(auth.sessionEpoch).toBe(originalEpoch + 1)
    expect(auth.identityScopeKey).not.toBe(originalScope)
    auth.$dispose()
    // jsdom queues 0ms storage events for localStorage writes/removals.
    // Drain only that turn, leaving any leaked refresh timeout/interval observable.
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(0)
    const userRefreshes = mockGetCurrentUser.mock.calls.length
    const tokenRefreshes = mockRefreshToken.mock.calls.length
    await vi.advanceTimersByTimeAsync(3600_000)
    expect(mockGetCurrentUser).toHaveBeenCalledTimes(userRefreshes)
    expect(mockRefreshToken).toHaveBeenCalledTimes(tokenRefreshes)
  })

  it('rejects an old successful login after a replacement login has completed', async () => {
    const auth = useAuthStore()
    let resolveOld!: (value: typeof fakeAuthResponse) => void
    mockLogin.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const old = auth.login({ email: fakeUser.email, password: 'old' })
    const rejected = expect(old).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, access_token: 'new-session' })
    await auth.login({ email: fakeUser.email, password: 'new' })
    resolveOld(fakeAuthResponse)
    await rejected
    expect(auth.token).toBe('new-session')
    expect(localStorage.getItem('auth_token')).toBe('new-session')
    auth.$dispose()
  })

  it.each([429, 500, 503])('retains the current identity when refreshing fails temporarily with %s', async status => {
    const auth = useAuthStore()
    mockLogin.mockResolvedValueOnce(fakeAuthResponse)
    await auth.login({ email: fakeUser.email, password: 'secret' })
    const scope = auth.identityScopeKey
    mockGetCurrentUser.mockRejectedValueOnce({ status, code: 'TOKEN_REFRESH_UNAVAILABLE' })
    await expect(auth.refreshUser()).rejects.toMatchObject({ status })
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.identityScopeKey).toBe(scope)
    expect(localStorage.getItem('refresh_token')).toBe(fakeAuthResponse.refresh_token)
    auth.$dispose()
  })

  it('clears memory and timers for a peer-tab logout and updates permission scope', async () => {
    const auth = useAuthStore()
    mockLogin.mockResolvedValueOnce(fakeAuthResponse)
    await auth.login({ email: fakeUser.email, password: 'secret' })
    const scope = auth.identityScopeKey
    auth.user!.policy_version = 2
    expect(auth.identityScopeKey).not.toBe(scope)
    localStorage.removeItem('auth_token')
    localStorage.removeItem('auth_user')
    auth.checkAuth()
    expect(auth.isAuthenticated).toBe(false)
    // jsdom queues 0ms storage events for localStorage writes/removals.
    // Drain only that turn, leaving any leaked refresh timeout/interval observable.
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(0)
    const userRefreshes = mockGetCurrentUser.mock.calls.length
    const tokenRefreshes = mockRefreshToken.mock.calls.length
    await vi.advanceTimersByTimeAsync(3600_000)
    expect(mockGetCurrentUser).toHaveBeenCalledTimes(userRefreshes)
    expect(mockRefreshToken).toHaveBeenCalledTimes(tokenRefreshes)
  })

  it('does not inherit a previous login refresh credential or run mode', async () => {
    const auth = useAuthStore()
    mockLogin.mockResolvedValueOnce({ ...fakeAuthResponse, user: { ...fakeUser, run_mode: 'simple' } })
    await auth.login({ email: fakeUser.email, password: 'secret' })
    mockLogin.mockResolvedValueOnce({ access_token: 'legacy-login', token_type: 'Bearer', user: fakeUser })
    await auth.login({ email: fakeUser.email, password: 'secret' })
    expect(localStorage.getItem('refresh_token')).toBeNull()
    expect(localStorage.getItem('token_expires_at')).toBeNull()
    expect(auth.runMode).toBe('standard')
    auth.$dispose()
  })
})
