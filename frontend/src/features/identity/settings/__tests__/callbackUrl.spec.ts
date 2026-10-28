import { describe, expect, it, vi } from 'vitest'
import { buildApiCallbackUrl } from '../callbackUrl'
vi.mock('@/shared/http', () => ({ buildApiUrl: (path: string) => '/api/v1' + path }))
describe('OAuth callback suggestions', () => {
  it('uses the configured management API and does not consume public API display settings', () => {
    expect(buildApiCallbackUrl('/auth/oauth/oidc/callback')).toBe(window.location.origin + '/api/v1/auth/oauth/oidc/callback')
  })
})
