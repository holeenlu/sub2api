import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

describe('HTTP service addresses', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
    vi.stubEnv('VITE_API_BASE_URL', '')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it.each([
    { configured: '', expected: '/api/v1' },
    { configured: 'api/v1', expected: '/api/v1' },
    { configured: ' /control/api/// ', expected: '/control/api' },
    { configured: 'https://admin.example/control/api///', expected: 'https://admin.example/control/api' },
    { configured: '//admin.example/api/v1/', expected: '//admin.example/api/v1' }
  ])('normalizes the configured API base: $configured', async ({ configured, expected }) => {
    vi.stubEnv('VITE_API_BASE_URL', configured)
    const { getAPIBaseURL } = await import('@/shared/http/url')

    expect(getAPIBaseURL()).toBe(expected)
  })

  it('keeps callback query data and avoids duplicating an API prefix', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '/control/api')
    const { buildApiUrl } = await import('@/shared/http/url')

    expect(buildApiUrl('auth/oauth/github/callback?code=abc')).toBe('/control/api/auth/oauth/github/callback?code=abc')
    expect(buildApiUrl('/api/v1/auth/me')).toBe('/control/api/auth/me')
    expect(buildApiUrl('/api/v1')).toBe('/control/api')
  })

  it('resolves gateway paths against the management service origin', async () => {
    vi.stubEnv('VITE_API_BASE_URL', 'https://admin.example:8443/control/api')
    const { buildGatewayUrl } = await import('@/shared/http/url')

    expect(buildGatewayUrl('v1/usage?range=day')).toBe('https://admin.example:8443/v1/usage?range=day')
    expect(buildGatewayUrl('/setup/status')).toBe('https://admin.example:8443/setup/status')
  })

  it('forces the supplier portal to use its own origin even with an external API build value', async () => {
    vi.stubEnv('VITE_VENDOR_PORTAL', 'true')
    vi.stubEnv('VITE_API_BASE_URL', 'https://platform.example/api/v1')
    const { getAPIBaseURL, buildApiUrl, buildGatewayUrl } = await import('@/shared/http/url')

    expect(getAPIBaseURL()).toBe('/api/v1')
    expect(buildApiUrl('/auth/me')).toBe('/api/v1/auth/me')
    expect(buildGatewayUrl('/v1/usage')).toBe(window.location.origin + '/v1/usage')
  })

  it('does not use the website API display link as an authenticated transport address', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '/control/api')
    vi.stubGlobal('__APP_CONFIG__', { api_base_url: 'https://public-display.example/v1' })
    const { getAPIBaseURL, buildApiUrl } = await import('@/shared/http/url')

    expect(getAPIBaseURL()).toBe('/control/api')
    expect(buildApiUrl('/auth/refresh')).toBe('/control/api/auth/refresh')
  })
})
