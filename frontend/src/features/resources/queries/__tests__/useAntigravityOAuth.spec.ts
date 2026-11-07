import { describe, expect, it, vi } from 'vitest'

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const antigravityApiFixture = vi.hoisted(() => ({
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      refreshAntigravityToken: vi.fn()
    }))

import { useAntigravityOAuth } from '@/features/resources/queries/useAntigravityOAuth'

describe('useAntigravityOAuth.buildCredentials', () => {
  it('falls back to the submitted refresh token when the response omits it', () => {
    const oauth = useAntigravityOAuth()

    const credentials = oauth.buildCredentials(
      {
        access_token: 'access-token',
        expires_at: 1_900_000_000
      },
      'submitted-refresh-token'
    )

    expect(credentials.refresh_token).toBe('submitted-refresh-token')
  })

  it('prefers a new refresh token returned by the response', () => {
    const oauth = useAntigravityOAuth()

    const credentials = oauth.buildCredentials(
      {
        access_token: 'access-token',
        refresh_token: 'rotated-refresh-token',
        expires_at: 1_900_000_000
      },
      'submitted-refresh-token'
    )

    expect(credentials.refresh_token).toBe('rotated-refresh-token')
  })
})

// This builder is shared by OAuth creation, refresh-token import and reauthorization.
describe('Antigravity subscription metadata', () => {
  it.each(['pro', 'ultra', 'free'])('preserves the upstream %s plan when saving credentials', (plan) => {
    const oauth = useAntigravityOAuth()
    const credentials = oauth.buildCredentials({ plan_type: plan })
    expect(credentials.plan_type).toBe(plan)
  })

  it('does not invent a plan when subscription discovery is unavailable', () => {
    const credentials = useAntigravityOAuth().buildCredentials({})
    expect(credentials).not.toHaveProperty('plan_type')
  })
})



vi.mock('@/features/resources/services', async () => {
const actual = await vi.importActual<typeof import('@/features/resources/services')>('@/features/resources/services')
const { editorTestServices } = await import('../../__tests__/editorTestServices')

const { useAppStore } = await import('@/app/model/app')
return { ...actual, useResourceEditorServices: () => { const services = editorTestServices()
services.notices = { ...services.notices, ...useAppStore() }


if (antigravityApiFixture) services.credentialAuthorization.antigravity = antigravityApiFixture


return services } }
})
