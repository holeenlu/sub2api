import { describe, expect, it, vi } from 'vitest'

import {
  buildClaudeSetupTokenCredentials,
  normalizeClaudeSetupToken
} from '../useAccountOAuth'

describe('Claude setup-token helpers', () => {
  it('accepts either a raw token or the documented export command', () => {
    expect(normalizeClaudeSetupToken('  sk-ant-oat01-example  ')).toBe('sk-ant-oat01-example')
    expect(normalizeClaudeSetupToken('export CLAUDE_CODE_OAUTH_TOKEN="sk-ant-oat01-example"')).toBe(
      'sk-ant-oat01-example'
    )
  })

  it('stores OAuth credentials with a one-year expiry', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-06T00:00:00Z'))

    const credentials = buildClaudeSetupTokenCredentials('sk-ant-oat01-example')

    expect(credentials).toMatchObject({
      access_token: 'sk-ant-oat01-example',
      token_type: 'oauth',
      scope: 'user:inference'
    })
    expect(credentials?.expires_at).toBe(Math.floor(new Date('2027-10-06T00:00:00Z').getTime() / 1000))
    vi.useRealTimers()
  })
})
