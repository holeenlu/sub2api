import { describe, expect, it } from 'vitest'

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
