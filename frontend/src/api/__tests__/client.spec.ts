import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import type { AxiosInstance } from 'axios'

// 需要在导入 client 之前设置 mock
vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN',
  i18n: { global: { t: (key: string) => key === 'ui.networkError' ? 'Network error. Please check your connection.' : key } },
}))

function sessionToken(session: string, userID = 7, issuedAt = 0): string {
  return `header.${btoa(JSON.stringify({ user_id: userID, role: 'user', sid: session, token_version: 0, iat: issuedAt }))}.signature`
}

describe('API Client', () => {
  let apiClient: AxiosInstance

  beforeEach(async () => {
    localStorage.clear()
    sessionStorage.clear()
    window.history.replaceState({}, '', '/')
    // 每次测试重新导入以获取干净的模块状态
    vi.resetModules()
    const mod = await import('@/api/client')
    apiClient = mod.apiClient
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  // --- 请求拦截器 ---

  describe('请求拦截器', () => {
    it('规范化相对 API base，避免在回调页拼出相对 v1 路径', async () => {
      vi.resetModules()
      vi.stubEnv('VITE_API_BASE_URL', 'api/v1')

      const mod = await import('@/api/client')

      expect(mod.apiClient.defaults.baseURL).toBe('/api/v1')
      expect(mod.buildApiUrl('/auth/oauth/github/callback?code=abc')).toBe(
        '/api/v1/auth/oauth/github/callback?code=abc'
      )
    })

    it('自动附加 Authorization 头', async () => {
      localStorage.setItem('auth_token', 'my-jwt-token')

      // 拦截实际请求
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBe('Bearer my-jwt-token')
    })

    it('无 token 时不附加 Authorization 头', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBeFalsy()
    })

    it('GET 请求自动附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.params).toHaveProperty('timezone')
    })

    it('POST 请求不附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/test', { foo: 'bar' })

      const config = adapter.mock.calls[0][0]
      expect(config.params?.timezone).toBeUndefined()
    })

    it('请求默认带 withCredentials 以支持跨域 cookie', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/auth/oauth/bind-token')

      const config = adapter.mock.calls[0][0]
      expect(config.withCredentials).toBe(true)
    })

    it('Admin API 在进入管理页面前也带 Admin UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/admin/users')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
    })

    it('管理页面调用共享 API 时带 Admin UI 标记', async () => {
      window.history.replaceState({}, '', '/admin/dashboard')
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/groups/available')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
    })

    it('普通用户页面调用共享 API 时不带 Admin UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/groups/available')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBeFalsy()
    })

    it('用户侧 timing API 自动带 User UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/auth/me')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-User-UI-Request')).toBe('1')
      expect(config.headers.get('X-Admin-UI-Request')).toBeFalsy()
    })

    it('支付用户 API 带 User UI 标记，公开支付 API 不带', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/payment/plans')
      expect(adapter.mock.calls[0][0].headers.get('X-User-UI-Request')).toBe('1')

      await apiClient.post('/payment/public/orders/verify', {})
      expect(adapter.mock.calls[1][0].headers.get('X-User-UI-Request')).toBeFalsy()
    })

    it('管理页调用共享 API 时同时带 Admin 与 User UI 标记', async () => {
      window.history.replaceState({}, '', '/admin/dashboard')
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/keys')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
      expect(config.headers.get('X-User-UI-Request')).toBe('1')
    })
  })

  // --- 响应拦截器 ---

  describe('响应拦截器', () => {
    it('code=0 时解包 data 字段', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: { name: 'test' }, message: 'ok' },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      const response = await apiClient.get('/test')
      expect(response.data).toEqual({ name: 'test' })
    })

    it('code!=0 时拒绝并返回结构化错误', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 1001, message: '参数错误', data: null },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          code: 1001,
          message: '参数错误',
        })
      )
    })

    it('部署与运营合规未确认时广播事件且保留登录态', async () => {
      localStorage.setItem('auth_token', 'admin-token')
      const listener = vi.fn()
      window.addEventListener('admin-compliance-required', listener)

      const adapter = vi.fn().mockRejectedValue({
        response: {
          status: 423,
          data: {
            code: 'ADMIN_COMPLIANCE_ACK_REQUIRED',
            message: 'administrator compliance acknowledgement is required',
            metadata: {
              version: 'v2026.06.10',
              document_path_zh: 'docs/legal/admin-compliance.zh.md',
              document_path_en: 'docs/legal/admin-compliance.en.md',
            },
          },
        },
        config: {
          url: '/admin/users',
          headers: { Authorization: 'Bearer admin-token' },
        },
        code: 'ERR_BAD_REQUEST',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/admin/users')).rejects.toEqual(
        expect.objectContaining({
          status: 423,
          code: 'ADMIN_COMPLIANCE_ACK_REQUIRED',
          metadata: expect.objectContaining({
            version: 'v2026.06.10',
          }),
        })
      )

      expect(listener).toHaveBeenCalledTimes(1)
      expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual(
        expect.objectContaining({
          version: 'v2026.06.10',
        })
      )
      expect(localStorage.getItem('auth_token')).toBe('admin-token')

      window.removeEventListener('admin-compliance-required', listener)
    })
  })

  // --- 401 Token 刷新 ---

  describe('401 Token 刷新', () => {
    it('无 refresh_token 时 401 清除 localStorage', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      // 不设置 refresh_token

      // Mock window.location
      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: { ...originalLocation, pathname: '/dashboard', href: '/dashboard' },
        writable: true,
      })

      const adapter = vi.fn().mockRejectedValue({
        response: {
          status: 401,
          data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
        },
        config: {
          url: '/test',
          headers: { Authorization: 'Bearer expired-token' },
        },
        code: 'ERR_BAD_REQUEST',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toBeDefined()

      expect(localStorage.getItem('auth_token')).toBeNull()

      // 恢复 location
      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
      })
    })

    it('有 refresh_token 时刷新并重试原请求', async () => {
      localStorage.setItem('auth_token', sessionToken('same-session'))
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() - 1))
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))

      const adapter = vi.fn()
        .mockImplementationOnce(async config => { throw {
          response: {
            status: 401,
            data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
          },
          config,
          code: 'ERR_BAD_REQUEST',
        } })
        .mockResolvedValueOnce({
          status: 200,
          data: { code: 0, data: { ok: true } },
          headers: {},
          config: {},
          statusText: 'OK',
        })
      apiClient.defaults.adapter = adapter
      vi.spyOn(axios, 'post').mockResolvedValueOnce({
        data: {
          code: 0,
          message: 'ok',
          data: {
            access_token: sessionToken('same-session', 7, 1),
            refresh_token: 'new-refresh-token',
            expires_in: 3600,
            token_type: 'Bearer',
          },
        },
      })

      await expect(apiClient.get('/test')).resolves.toMatchObject({ data: { ok: true } })

      expect(adapter).toHaveBeenCalledTimes(2)
      expect(localStorage.getItem('auth_token')).toBe(sessionToken('same-session', 7, 1))
      expect(localStorage.getItem('refresh_token')).toBe('new-refresh-token')
      expect(adapter.mock.calls[1][0].headers.get('Authorization')).toBe(`Bearer ${sessionToken('same-session', 7, 1)}`)
    })

    it.each([429, 500, 503, 0])('刷新暂时失败（%s）时保留会话并返回实际状态', async (status) => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
      localStorage.setItem('token_expires_at', '123')
      const refreshError = new axios.AxiosError('Refresh unavailable', 'ERR_NETWORK')
      if (status) {
        refreshError.response = {
          status, data: { message: 'Please try again later' }, statusText: '',
          headers: {}, config: { headers: new axios.AxiosHeaders() },
        }
      }
      const refresh = await import('@/api/tokenRefresh')
      vi.spyOn(refresh, 'refreshAuthTokens').mockRejectedValueOnce(refreshError)
      const adapter = vi.fn().mockRejectedValueOnce({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toMatchObject({
        status, code: 'TOKEN_REFRESH_UNAVAILABLE',
        message: status ? 'Please try again later' : 'Refresh unavailable',
      })
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(localStorage.getItem('auth_token')).toBe('expired-token')
      expect(localStorage.getItem('refresh_token')).toBe('refresh-token')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify({ id: 7 }))
      expect(localStorage.getItem('token_expires_at')).toBe('123')
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
      expect(window.location.pathname).toBe('/')
    })

    it.each([401, 403, null])('刷新被拒绝（%s）时仍清除失效会话', async (status) => {
      window.history.replaceState({}, '', '/login')
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
      localStorage.setItem('token_expires_at', '123')
      const refreshError = status
        ? Object.assign(new axios.AxiosError('Refresh rejected'), { response: { status } })
        : new Error('Invalid refresh response')
      const refresh = await import('@/api/tokenRefresh')
      vi.spyOn(refresh, 'refreshAuthTokens').mockRejectedValueOnce(refreshError)
      apiClient.defaults.adapter = vi.fn().mockRejectedValueOnce({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
      })

      await expect(apiClient.get('/test')).rejects.toMatchObject({ status: 401, code: 'TOKEN_REFRESH_FAILED' })
      for (const key of ['auth_token', 'refresh_token', 'auth_user', 'token_expires_at']) {
        expect(localStorage.getItem(key)).toBeNull()
      }
      expect(sessionStorage.getItem('auth_expired')).toBe('1')
    })

    it('刷新期间换号时旧请求不会清除新会话', async () => {
      localStorage.setItem('auth_token', 'user-a-access')
      localStorage.setItem('refresh_token', 'user-a-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() - 1))
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))

      apiClient.defaults.adapter = vi.fn().mockImplementationOnce(async config => { throw {
        response: {
          status: 401,
          data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
        },
        config,
        code: 'ERR_BAD_REQUEST',
      } })

      let rejectRefresh!: (reason: Error) => void
      vi.spyOn(axios, 'post').mockImplementationOnce(
        () => new Promise((_resolve, reject) => {
          rejectRefresh = reject
        })
      )

      const staleRequest = apiClient.get('/test')
      await vi.waitFor(() => expect(axios.post).toHaveBeenCalledTimes(1))

      localStorage.setItem('auth_token', 'user-b-access')
      localStorage.setItem('refresh_token', 'user-b-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
      rejectRefresh(new Error('stale refresh failed'))

      await expect(staleRequest).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      expect(localStorage.getItem('auth_token')).toBe('user-b-access')
      expect(localStorage.getItem('refresh_token')).toBe('user-b-refresh')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify({ id: 8 }))
      expect(window.location.pathname).toBe('/')
    })
  })

  describe('请求与逻辑会话绑定', () => {
    it.each([200, 401, 403, 423])('换号后的迟到 %s 不回写、不重试、不改变新会话', async status => {
      localStorage.setItem('auth_token', sessionToken('session-a'))
      localStorage.setItem('refresh_token', 'refresh-a')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'user' }))
      const refresh = await import('@/api/tokenRefresh')
      const refreshSpy = vi.spyOn(refresh, 'refreshAuthTokens')
      const events = vi.spyOn(window, 'dispatchEvent')
      let finish!: () => void
      const adapter = vi.fn(config => new Promise((resolve, reject) => {
        finish = () => {
          const response = { status, headers: {}, config, statusText: '', data: { code: status === 423 ? 'ADMIN_COMPLIANCE_ACK_REQUIRED' : 0, data: { private: 'user-a' } } }
          if (status === 200) resolve(response)
          else reject({ response, config })
        }
      }))
      apiClient.defaults.adapter = adapter
      const pending = apiClient.post('/keys', { name: 'old-user-key' })
      const rejected = expect(pending).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      await vi.waitFor(() => expect(adapter).toHaveBeenCalledTimes(1))
      localStorage.setItem('auth_token', sessionToken('session-b', 8))
      localStorage.setItem('refresh_token', 'refresh-b')
      localStorage.setItem('auth_user', JSON.stringify({ id: 8, role: 'user' }))
      finish()
      await rejected
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(refreshSpy).not.toHaveBeenCalled()
      expect(events).not.toHaveBeenCalled()
      expect(localStorage.getItem('refresh_token')).toBe('refresh-b')
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
    })

    it('相同用户重新登录后不接受旧会话结果，但同会话令牌轮换允许完成', async () => {
      for (const sameSession of [false, true]) {
        localStorage.setItem('auth_token', sessionToken('initial'))
        localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'user' }))
        apiClient.defaults.adapter = async config => {
          localStorage.setItem('auth_token', sessionToken(sameSession ? 'initial' : 'new-login', 7, 1))
          return { status: 200, data: { code: 0, data: { ok: true } }, config, headers: {}, statusText: 'OK' }
        }
        if (sameSession) await expect(apiClient.get('/usage')).resolves.toMatchObject({ data: { ok: true } })
        else await expect(apiClient.get('/usage')).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      }
    })
  })

  // --- 网络错误 ---

  describe('网络错误', () => {
    it.each(['ERR_NETWORK', 'ECONNABORTED', 'ETIMEDOUT', undefined])('网络错误保留错误码 %s', async (code) => {
      const adapter = vi.fn().mockRejectedValue({
        code,
        message: 'Network Error',
        config: { url: '/test' },
        // 没有 response
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          status: 0,
          code: code || 'ERR_NETWORK',
          message: 'Network error. Please check your connection.',
        })
      )
    })
  })

  // --- 请求取消 ---

  describe('请求取消', () => {
    it('取消的请求保持原始取消错误', async () => {
      const source = axios.CancelToken.source()

      const adapter = vi.fn().mockRejectedValue(
        new axios.Cancel('Operation canceled')
      )
      apiClient.defaults.adapter = adapter

      await expect(
        apiClient.get('/test', { cancelToken: source.token })
      ).rejects.toBeDefined()
    })
  })
})

describe('Supplier client boundaries',()=>{
 afterEach(()=>{vi.unstubAllEnvs();localStorage.clear()})
 it('preserves an explicit reconciliation timezone',async()=>{
  vi.resetModules();const {apiClient}=await import('@/api/client')
  const adapter=vi.fn(async config=>({status:200,data:{code:0,data:{}},headers:{},config,statusText:'OK'}));apiClient.defaults.adapter=adapter
  await apiClient.get('/vendor/usage/summary',{params:{timezone:'Asia/Tokyo'}})
  expect(adapter.mock.calls[0][0].params.timezone).toBe('Asia/Tokyo')
 })
 it('forces same-origin API requests for a dedicated supplier build',async()=>{
  vi.resetModules();vi.stubEnv('VITE_VENDOR_PORTAL','true');vi.stubEnv('VITE_API_BASE_URL','https://admin.example/api/v1')
  const {apiClient}=await import('@/api/client');expect(apiClient.defaults.baseURL).toBe('/api/v1')
 })
 it('discards sensitive supplier results after the login session changes',async()=>{
  vi.resetModules();localStorage.setItem('auth_user',JSON.stringify({id:5,role:'user'}));localStorage.setItem('auth_token','old-session')
  const {apiClient}=await import('@/api/client')
  apiClient.defaults.adapter=async config=>{localStorage.setItem('auth_token','new-session');return {status:200,data:new Blob(['sensitive-export']),headers:{},config,statusText:'OK'}}
  await expect(apiClient.post('/vendor/accounts/export',{ids:[1]},{responseType:'blob'})).rejects.toMatchObject({code:'ERR_CANCELED'})
 })
})
