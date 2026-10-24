import catalog from '../../../backend/internal/pkg/claude/prompts/catalog.json'

export type ClaudePromptPreset = 'official' | 'official_full' | 'minimal' | 'zero' | 'custom'
export interface ClaudePromptBlock {
  id?: string
  note?: string
  hide?: boolean
  match?: string
  placement?: 'system' | 'overlay_body' | 'overlay_wrapper'
  enabled?: boolean
  type?: 'text'
  text?: string
  drop_if_empty?: boolean
  cache_control?: boolean | null | { type: 'ephemeral'; ttl?: '5m' | '1h'; [key: string]: unknown }
}

export const claudePromptVariables = Object.keys(catalog.variables).map(name => '{' + name + '}')

export function defaultClaudePromptBlocks(): ClaudePromptBlock[] {
  return structuredClone(catalog.presets.default) as ClaudePromptBlock[]
}

export function claudePromptPresetBlocks(preset: Exclude<ClaudePromptPreset, 'custom'>): ClaudePromptBlock[] {
  return structuredClone(catalog.presets[preset]) as ClaudePromptBlock[]
}

export function parseClaudePromptBlocks(raw: string): ClaudePromptBlock[] {
  if (!raw.trim()) return defaultClaudePromptBlocks()
  let value: unknown
  try { value = JSON.parse(raw) } catch {
    value = raw.split(/\r?\n/).map(line => line.trim()).filter(Boolean).map(line => JSON.parse(line))
  }
  if (value && typeof value === 'object' && !Array.isArray(value) && 'text' in value && !('blocks' in value)) value = [value]
  const blocks = Array.isArray(value) ? value
    : value && typeof value === 'object' && Object.keys(value).every(key => key === 'blocks')
      ? (value as { blocks?: unknown }).blocks : undefined
  if (!Array.isArray(blocks)) throw new Error('shape')
  const allowed = ['enabled', 'type', 'text', 'drop_if_empty', 'cache_control', 'id', 'note', 'hide', 'placement', 'match']
  for (const block of blocks) {
    if (!block || typeof block !== 'object' || Array.isArray(block) || Object.keys(block).some(key => !allowed.includes(key)) ||
        (block.type !== undefined && block.type !== '' && block.type !== 'text') ||
        (block.text !== undefined && typeof block.text !== 'string') ||
        (block.enabled !== undefined && typeof block.enabled !== 'boolean') ||
        (block.drop_if_empty !== undefined && typeof block.drop_if_empty !== 'boolean') ||
        (block.hide !== undefined && typeof block.hide !== 'boolean') ||
        (block.match !== undefined && typeof block.match !== 'string') ||
        (block.id !== undefined && typeof block.id !== 'string') ||
        (block.note !== undefined && typeof block.note !== 'string') ||
        (block.placement !== undefined && !['system', 'overlay_body', 'overlay_wrapper'].includes(block.placement))) throw new Error('shape')
    if (block.text) block.text = block.text.replace(/\{\{?\s*([a-z_]+)\s*\}\}?/g, '{$1}')
  }
  return blocks
}

export function claudePromptValidationError(blocks: ClaudePromptBlock[]): string | null {
  let callerSlots = 0
  let markers = 0
  let wrappers = 0
  if (blocks.length > 24 || JSON.stringify(blocks).length > 200000) return 'budgetError'
  for (const block of blocks) {
    const cache = block.cache_control
    if (cache != null && typeof cache !== 'boolean' &&
        (typeof cache !== 'object' || Array.isArray(cache) || cache.type !== 'ephemeral' ||
         (cache.ttl !== undefined && cache.ttl !== '5m' && cache.ttl !== '1h'))) return 'cacheError'
    if (block.match && (block.placement !== 'overlay_body' || block.match.length > 512)) return 'callerError'
    const text = block.text ?? ''
    if ([...text.matchAll(/\{\{?\s*([a-z_]+)\s*\}\}?/g)].some(match => !Object.prototype.hasOwnProperty.call(catalog.variables, match[1]))) return 'callerError'
    if (text.includes('{dynamic_boundary}') && (text.trim() !== '{dynamic_boundary}' || (block.placement && block.placement !== 'system') || cache)) return 'callerError'

    if (text.length > 40000 || (block.id?.length ?? 0) > 64 || (block.note?.length ?? 0) > 2048) return 'budgetError'
    if (block.placement && block.placement !== 'system' && (cache || text.includes('{caller_system}'))) return 'callerError'
    if (block.placement === 'overlay_wrapper') {
      if (!text.includes('{overlay_body}')) return 'callerError'
      if (block.enabled !== false) wrappers++
    }
    if (text.includes('{caller_system}') && (text.trim() !== '{caller_system}' || cache)) return 'callerError'
    if (block.enabled === false) continue
    if (text.trim() === '{caller_system}') callerSlots++
    if (cache && text.trim()) markers++
  }
  if (callerSlots > 1) return 'callerError'
  if (markers > 4 || wrappers > 1) return 'budgetError'
  return null
}

export function serializeClaudePromptBlocks(blocks: ClaudePromptBlock[]): string {
  return JSON.stringify(blocks.map(block => {
    const result = { ...block, enabled: block.enabled !== false, type: 'text' }
    // Preserve absent TTL and extra supported cache attributes on read/save.
    if (result.cache_control === true) result.cache_control = { type: 'ephemeral', ttl: '5m' }
    return result
  }), null, 2)
}

export function detectClaudePromptPreset(blocks: ClaudePromptBlock[]): ClaudePromptPreset {
  const normalized = (items: ClaudePromptBlock[]) => JSON.stringify(items.map(block => ({
    enabled: block.enabled !== false, placement: block.placement ?? 'system', text: block.text ?? '', drop: block.drop_if_empty ?? true,
    cache: block.cache_control ? Object.fromEntries(Object.entries(
      block.cache_control === true ? { type: 'ephemeral', ttl: '5m' } : block.cache_control
    ).sort(([left], [right]) => left.localeCompare(right))) : null
  })))
  for (const preset of ['zero', 'official', 'official_full', 'minimal'] as const) {
    if (normalized(blocks) === normalized(claudePromptPresetBlocks(preset))) return preset
  }
  return 'custom'
}
