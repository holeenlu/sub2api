import { describe, expect, it } from 'vitest'
import {
  claudePromptPresetBlocks, claudePromptValidationError, defaultClaudePromptBlocks,
  detectClaudePromptPreset, parseClaudePromptBlocks, serializeClaudePromptBlocks
} from '../claudePrompt'

describe('Claude prompt persistence', () => {
  it('distinguishes inherited defaults from explicit zero without a disabled placeholder', () => {
    expect(parseClaudePromptBlocks('')).toEqual(defaultClaudePromptBlocks())
    expect(parseClaudePromptBlocks('[]')).toEqual([])
    expect(parseClaudePromptBlocks('{"blocks":[]}')).toEqual([])
    expect(serializeClaudePromptBlocks([])).toBe('[]')
  })

  it.each(['official', 'official_full', 'minimal', 'zero'] as const)('detects %s from saved blocks alone', preset => {
    const blocks = parseClaudePromptBlocks(serializeClaudePromptBlocks(claudePromptPresetBlocks(preset)))
    expect(detectClaudePromptPreset(blocks)).toBe(preset)
    expect(claudePromptValidationError(blocks)).toBeNull()
  })

  it('retains variables, explicit omission and cache attributes when editing a saved envelope', () => {
    const blocks = parseClaudePromptBlocks('{"blocks":[{"text":"{claude_code_expansion_prompt}","drop_if_empty":false,"cache_control":{"type":"ephemeral","scope":"global"}}]}')
    expect(JSON.parse(serializeClaudePromptBlocks(blocks))).toEqual([{
      type: 'text', enabled: true, text: '{claude_code_expansion_prompt}', drop_if_empty: false,
      cache_control: { type: 'ephemeral', scope: 'global' }
    }])
  })

  it('recognizes an unchanged preset when cache JSON keys have a different order', () => {
    const blocks = claudePromptPresetBlocks('official_full')
    const cachedBlock = blocks.find(block => block.cache_control)
    expect(cachedBlock).toBeDefined()
    cachedBlock!.cache_control = { ttl: '1h', type: 'ephemeral' }
    expect(detectClaudePromptPreset(blocks)).toBe('official_full')
  })

  it.each(['null', '{}', '{"blocks":null}', '[null]', '[{"type":"image"}]', '{"blocks":[],"unknown":true}'])('rejects %s without substituting defaults', raw => {
    expect(() => parseClaudePromptBlocks(raw)).toThrow()
  })

  it.each([
    ['[{"text":"x","cache_control":{"type":"ephemeral","ttl":"30m"}}]', 'cacheError'],
    ['[{"text":"x","cache_control":{"type":"ephemeral","ttl":null}}]', 'cacheError'],
    ['[{"text":"x","cache_control":{"type":"persistent"}}]', 'cacheError'],
    ['[{"text":"prefix {caller_system}"}]', 'callerError'],
    ['[{"text":"{unknown_variable}"}]', 'callerError'],
    ['[{"text":"{dynamic_boundary}","cache_control":true}]', 'callerError'],
    ['[{"text":"{caller_system}","cache_control":true}]', 'callerError'],
    ['[{"text":"{caller_system}"},{"text":"{caller_system}"}]', 'callerError'],
  ])('validates %s', (raw, error) => {
    expect(claudePromptValidationError(parseClaudePromptBlocks(raw))).toBe(error)
  })

  it('checks the enabled marker budget and permits disabled drafts', () => {
    const blocks = Array.from({ length: 5 }, () => ({ text: 'cacheable', cache_control: true, enabled: true }))
    expect(claudePromptValidationError(blocks)).toBe('budgetError')
    blocks[4].enabled = false
    expect(claudePromptValidationError(blocks)).toBeNull()
  })
})

it('imports JSONL metadata and overlay placement without changing zero injection', () => {
 const blocks = parseClaudePromptBlocks('{"id":"env","note":"source","hide":true,"text":"{{env}}"}\n{"placement":"overlay_body","text":"policy"}')
 expect(blocks[0]).toMatchObject({ id: 'env', note: 'source', hide: true, text: '{env}' })
 expect(claudePromptValidationError(blocks)).toBeNull()
 expect(claudePromptValidationError([{ text: ' ', drop_if_empty: false }])).toBeNull()
 expect(claudePromptPresetBlocks('minimal')).toHaveLength(5)
 expect(claudePromptPresetBlocks('zero')).toEqual([])
 expect(claudePromptValidationError([{ placement: 'overlay_wrapper', text: 'missing slot' }])).toBe('callerError')
})
