import { describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  buildClaudeSetupTokenCredentials,
  normalizeClaudeSetupToken,
  useAccountOAuth
} from '../useAccountOAuth'

describe('Claude setup-token helpers', () => {
  it('accepts either a raw token or the documented export command', () => {
    expect(normalizeClaudeSetupToken('  sk-ant-oat01-example  ')).toBe('sk-ant-oat01-example')
    expect(normalizeClaudeSetupToken('export CLAUDE_CODE_OAUTH_TOKEN="sk-ant-oat01-example"')).toBe(
      'sk-ant-oat01-example'
    )
  })

  it('stores OAuth credentials without inventing a local expiry timestamp', () => {
    const credentials = buildClaudeSetupTokenCredentials('sk-ant-oat01-example')

    expect(credentials).toMatchObject({
      access_token: 'sk-ant-oat01-example',
      token_type: 'oauth',
      scope: 'user:inference'
    })
    expect(credentials).not.toHaveProperty('expires_at')
  })

  it('rejects non Setup Token credentials', () => {
    expect(buildClaudeSetupTokenCredentials('sk-ant-api03-example')).toBeNull()
  })
})

const api = vi.hoisted(() => ({ generateAuthUrl: vi.fn() }))
const accountsApiFixture = vi.hoisted(() => (api))

describe('Claude authorization session isolation', () => {
  it('never starts browser OAuth for a pasted token and discards a superseded link', async () => {
    setActivePinia(createPinia())
    const oauth = useAccountOAuth()
    expect(await oauth.generateAuthUrl('setup-token-manual')).toBe(false)
    expect(api.generateAuthUrl).not.toHaveBeenCalled()
    let finish!: (value: { auth_url: string; session_id: string }) => void
    api.generateAuthUrl.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const pending = oauth.generateAuthUrl('oauth')
    oauth.resetState()
    finish({ auth_url: 'https://old.invalid', session_id: 'old-full-oauth' })
    expect(await pending).toBe(false)
    expect(oauth.sessionId.value).toBe('')
    api.generateAuthUrl.mockResolvedValueOnce({ auth_url: 'https://new.invalid', session_id: 'new-inference' })
    expect(await oauth.generateAuthUrl('setup-token')).toBe(true)
    expect(api.generateAuthUrl).toHaveBeenLastCalledWith('/admin/accounts/generate-setup-token-url', {})
    expect(oauth.sessionId.value).toBe('new-inference')
  })
})

vi.mock('@/features/resources/accounts', () => ({ ...accountsApiFixture, default: accountsApiFixture, accountsAPI: accountsApiFixture }))

vi.mock('@/features/resources/services', async () => {
const actual = await vi.importActual<typeof import('@/features/resources/services')>('@/features/resources/services')
const { editorTestServices } = await import('../../__tests__/editorTestServices')

return { ...actual, useResourceEditorServices: () => { const services = editorTestServices()





return services } }
})
