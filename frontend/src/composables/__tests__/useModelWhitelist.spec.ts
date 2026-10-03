import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { buildModelMappingObject, getPresetMappingsByPlatform, splitModelMappingObject } from '../useModelWhitelist'

describe('useModelWhitelist', () => {

  it('openai 预设映射包含 GPT-6 别名和 Astra', () => {
    expect(getPresetMappingsByPlatform('openai')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'GPT-6.1 Sol', from: 'gpt-6.1-sol', to: 'gpt-6.1-sol' }),
      expect.objectContaining({ label: 'GPT-6', from: 'gpt-6', to: 'gpt-6' }),
      expect.objectContaining({ label: 'GPT-6 Astra', from: 'gpt-6-astra', to: 'gpt-6-astra' })
    ]))
  })




  it('Claude Sonnet 5.5 预设使用各平台的官方模型 ID', () => {
    expect(getPresetMappingsByPlatform('claude')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'Sonnet 5.5', from: 'claude-sonnet-5-5', to: 'claude-sonnet-5-5' })
    ]))
    expect(getPresetMappingsByPlatform('bedrock')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'Sonnet 5.5', from: 'claude-sonnet-5-5', to: 'global.anthropic.claude-sonnet-5-5' })
    ]))
  })


  it('combined 模式支持 Grok 4.5 官方别名映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['grok-4.5'],
      [
        { from: 'grok-latest', to: 'grok-4.5' },
        { from: 'grok-4.5-latest', to: 'grok-4.5' },
        { from: 'grok-build-latest', to: 'grok-4.5' }
      ]
    )

    expect(mapping).toEqual({
      'grok-4.5': 'grok-4.5',
      'grok-latest': 'grok-4.5',
      'grok-4.5-latest': 'grok-4.5',
      'grok-build-latest': 'grok-4.5'
    })
  })





  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('combined 模式会同时保留白名单身份映射和模型映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('combined mode retains a mapping when a whitelist entry has the same source', () => {
    expect(buildModelMappingObject('combined', ['gpt-latest'], [{ from: 'gpt-latest', to: 'deepseek-chat' }])).toEqual({
      'gpt-latest': 'deepseek-chat'
    })
  })

  it('split mapping keeps only identity entries in the whitelist after reopening', () => {
    expect(splitModelMappingObject({ 'gpt-latest': 'deepseek-chat', 'gpt-5.4': 'gpt-5.4' })).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }]
    })
  })

  it('splitModelMappingObject 会把身份映射还原成白名单，其余保留为映射', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }]
    })
  })
})
