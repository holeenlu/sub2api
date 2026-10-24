import { describe, expect, it } from 'vitest'
import { cloudResultAnswer, isOpenAICloudAccount, readCloudTestResult } from '../openaiCloud'
import type { Account } from '@/types'

const account = { platform: 'openai', type: 'oauth', credentials: {} } as Account
const result = {
  kind: 'cloud_result', response_id: 'response-1', status: 'completed', model: 'model',
  output: [
    { type: 'function_call', name: 'echo', arguments: '{"text":"tool input"}' },
    { type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'Final answer' }] }
  ], usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15 }, created_at: 1, completed_at: 2,
  capabilities: { text: true, tools: true, usage: true, settlement: true, image_input: true, encrypted_context: true, web_search: true, image_generation: true }
}

describe('Cloud UI boundaries', () => {
  it('excludes supplier, shadow, PAT, Agent Identity and Excel accounts', () => {
    expect(isOpenAICloudAccount(account)).toBe(true)
    for (const overrides of [
      { vendor_id: 4 }, { parent_account_id: 2 }, { type: 'apikey' },
      { credentials: { auth_mode: 'personal_access_token' } },
      { credentials: { openai_auth_mode: 'personalAccessToken' } },
      { credentials: { auth_mode: 'agentIdentity' } },
      { credentials: { client_id: 'app_fnr0pYvVwwFDocDumLG3H2Bp' } }
    ]) expect(isOpenAICloudAccount({ ...account, ...overrides } as Account)).toBe(false)
  })

  it('exports only the terminal result and derives text from assistant output', () => {
    const parsed = readCloudTestResult({ ...result, headers: { Authorization: 'secret' }, callback: 'secret' })
    expect(parsed).not.toHaveProperty('headers')
    expect(parsed).not.toHaveProperty('callback')
    expect(cloudResultAnswer(parsed)).toBe('Final answer')
    expect(cloudResultAnswer(readCloudTestResult({ ...result, status: 'failed' }))).toBe('')
    expect(readCloudTestResult({ ...result, output: [null] })).toBeNull()
    expect(readCloudTestResult({ type: 'content', text: 'partial' })).toBeNull()
  })

  it('prefers declared final answers while preserving legacy extraction without a final phase', () => {
    const message = (role: string, phase: string | undefined, text: string) => ({
      type: 'message', role, ...(phase ? { phase } : {}), content: [{ type: 'output_text', text }]
    })
    const answer = (output: Array<Record<string, unknown>>) => cloudResultAnswer(readCloudTestResult({ ...result, output }))
    expect(answer([
      message('assistant', 'commentary', 'Working'), message('assistant', 'partial_answer', 'Partial'),
      message('assistant', undefined, 'Unknown'), message('assistant', 'final_answer', 'Answer'),
      message('assistant', 'final_answer', 'Two')
    ])).toBe('Answer\nTwo')
    expect(answer([
      message('assistant', 'commentary', 'Working'), message('assistant', 'partial_answer', 'Partial'),
      message('assistant', undefined, 'Unknown')
    ])).toBe('Working\nPartial\nUnknown')
    expect(answer([message('user', 'final_answer', 'User'), message('assistant', undefined, 'Legacy')])).toBe('Legacy')
    expect(answer([message('assistant', 'commentary', 'Working'), message('assistant', 'final_answer', '')])).toBe('')
  })
})
