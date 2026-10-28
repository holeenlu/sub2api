import { describe, expect, it } from 'vitest'

import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const en = assembledEnMessages.admin
const zh = assembledZhMessages.admin


describe('OpenAI WS mode locale descriptions', () => {
  it('documents the global v2 router requirement for account WS modes', () => {
    expect(zh.accounts.openai.wsModeDesc).toContain('mode_router_v2_enabled=true')
    expect(en.accounts.openai.wsModeDesc).toContain('mode_router_v2_enabled=true')
  })
})
