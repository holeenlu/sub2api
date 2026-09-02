import { describe, expect, it } from 'vitest'

import {
  CLAUDE_SETUP_TOKEN_INVALID_KEY,
  ClaudeSetupTokenError,
  buildClaudeSetupTokenCredentials,
  describeClaudeSetupTokenError,
  isClaudeSetupToken,
  parseClaudeSetupTokens
} from '@/composables/useAccountOAuth'

const VALID_TOKEN = 'sk-ant-oat01-' + 'a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8s9T0'
const OTHER_TOKEN = 'sk-ant-oat01-' + 'z9Y8x7W6v5U4t3S2r1Q0p9O8n7M6l5K4j3I2h1G0'

describe('buildClaudeSetupTokenCredentials', () => {
  it('stores only the long-lived inference bearer token', () => {
    expect(buildClaudeSetupTokenCredentials(`  ${VALID_TOKEN}  `)).toEqual({
      access_token: VALID_TOKEN,
      token_type: 'Bearer',
      scope: 'user:inference'
    })
  })

  it('never emits expires_at or refresh_token', () => {
    const credentials = buildClaudeSetupTokenCredentials(VALID_TOKEN)
    expect(credentials).not.toHaveProperty('expires_at')
    expect(credentials).not.toHaveProperty('refresh_token')
  })

  it.each([
    ['claude.ai sessionKey', 'sk-ant-sid01-' + 'a'.repeat(40)],
    ['API key', 'sk-ant-api03-' + 'a'.repeat(40)],
    ['truncated paste', 'sk-ant-oat01-abc'],
    ['two tokens on one line', `${VALID_TOKEN} ${OTHER_TOKEN}`],
    ['empty input', '   ']
  ])('rejects %s with an i18n-keyed error', (_label, raw) => {
    expect(isClaudeSetupToken(raw)).toBe(false)
    try {
      buildClaudeSetupTokenCredentials(raw)
      expect.unreachable('expected buildClaudeSetupTokenCredentials to throw')
    } catch (error) {
      expect(error).toBeInstanceOf(ClaudeSetupTokenError)
      expect((error as ClaudeSetupTokenError).messageKey).toBe(CLAUDE_SETUP_TOKEN_INVALID_KEY)
    }
  })
})

describe('parseClaudeSetupTokens', () => {
  it('parses one token per line and trims surrounding whitespace', () => {
    expect(parseClaudeSetupTokens(`\n${VALID_TOKEN}\n  ${OTHER_TOKEN}  \n`)).toEqual([
      VALID_TOKEN,
      OTHER_TOKEN
    ])
  })

  it('drops repeated tokens so one paste cannot create duplicate accounts', () => {
    expect(parseClaudeSetupTokens(`${VALID_TOKEN}\n${VALID_TOKEN}\r\n${OTHER_TOKEN}`)).toEqual([
      VALID_TOKEN,
      OTHER_TOKEN
    ])
  })
})

describe('describeClaudeSetupTokenError', () => {
  const t = (key: string) => `t:${key}`

  it('translates helper errors through their i18n key', () => {
    expect(
      describeClaudeSetupTokenError(new ClaudeSetupTokenError(CLAUDE_SETUP_TOKEN_INVALID_KEY), t, 'fallback')
    ).toBe(`t:${CLAUDE_SETUP_TOKEN_INVALID_KEY}`)
  })

  it('surfaces the API error message and falls back for unknown shapes', () => {
    expect(describeClaudeSetupTokenError({ status: 400, message: 'no refresh' }, t, 'fallback')).toBe(
      'no refresh'
    )
    expect(describeClaudeSetupTokenError(undefined, t, 'fallback')).toBe('fallback')
  })
})
