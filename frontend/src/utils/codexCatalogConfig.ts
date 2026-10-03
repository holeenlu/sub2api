export interface CodexCatalogReasoningLevel {
  effort?: unknown
}

export interface CodexCatalogModel {
  slug: string
  visibility?: unknown
  model_purpose?: unknown
  priority?: unknown
  default_reasoning_level?: unknown
  supported_reasoning_levels?: CodexCatalogReasoningLevel[]
  context_window?: unknown
  max_context_window?: unknown
  max_output_tokens?: unknown
  display_name?: unknown
}

function trimEffort(value: unknown): string {
  if (typeof value !== 'string') return ''
  return value.trim()
}

export function parseCodexCatalogModels(content: string | null | undefined): CodexCatalogModel[] {
  if (!content) return []
  try {
    const payload: unknown = JSON.parse(content)
    if (typeof payload !== 'object' || payload === null || !('models' in payload)) return []
    const models = (payload as { models?: unknown }).models
    if (!Array.isArray(models)) return []
    return models.flatMap((model) => {
      if (typeof model !== 'object' || model === null || !('slug' in model)) return []
      const slug = trimEffort((model as { slug?: unknown }).slug)
      if (!slug) return []
      return [{ ...(model as CodexCatalogModel), slug }]
    })
  } catch {
    return []
  }
}

export function findCodexCatalogModel(
  content: string | null | undefined,
  slug: string
): CodexCatalogModel | undefined {
  const wanted = slug.trim()
  if (!wanted) return undefined
  return parseCodexCatalogModels(content).find((model) => model.slug === wanted)
}

interface CodexModelRank {
  tier: number
  version: number[]
  snapshot: string
}

function codexModelName(slug: string): string {
  return slug.trim().split('/').at(-1)!.toLowerCase()
}

function isSelectableCodexModel(model: CodexCatalogModel): boolean {
  if (typeof model.slug !== 'string' || !model.slug.trim() || model.slug.includes('*')) return false
  if (trimEffort(model.visibility).toLowerCase() === 'hide') return false
  if (trimEffort(model.model_purpose).toLowerCase() === 'background') return false
  const name = codexModelName(model.slug)
  // Dedicated media/tool endpoints cannot serve as a Codex conversation model.
  if (/^(?:gpt-image|dall-e|sora|whisper|tts|text-embedding|omni-moderation|grok-imagine|imagen|veo)(?:-|$)/.test(name)) return false
  if (/^codex-auto-/.test(name) || name === 'gpt-reserve') return false
  return !/^(?:gpt|gemini|grok)-(?:.*-)?(?:image|video|audio|realtime|tts|transcribe|transcription|embedding|moderation)(?:-|$)/.test(name)
}

function rankCodexModel(slug: string): CodexModelRank | null {
  // Unknown suffixes (including specialized models) are not assumed to be flagships.
  const match = codexModelName(slug).match(
    /^gpt-(\d+(?:\.\d+)*)(?:-(astra|sol|terra|luna|mini|nano))?(?:-(\d{4}-\d{2}-\d{2}))?$/
  )
  if (!match) return null
  const version = match[1].split('.').map(Number)
  const tiers: Record<string, number> = { astra: 5, sol: 4, terra: 3, luna: 2, mini: 1, nano: 0 }
  // The documented GPT-6 alias is Astra; other plain GPT IDs are full-size models.
  const defaultTier = version[0] === 6 && version.slice(1).every((part) => part === 0) ? tiers.astra : tiers.sol
  const tier = match[2] ? tiers[match[2]] : defaultTier
  return { tier, version, snapshot: match[3] ?? '' }
}

function compareCodexModelRank(left: CodexModelRank, right: CodexModelRank): number {
  if (left.tier !== right.tier) return left.tier - right.tier
  for (let i = 0; i < Math.max(left.version.length, right.version.length); i += 1) {
    const difference = (left.version[i] ?? 0) - (right.version[i] ?? 0)
    if (difference !== 0) return difference
  }
  return left.snapshot.localeCompare(right.snapshot)
}

export function selectCodexConfigModel(
  models: CodexCatalogModel[],
  preferredModel: string
): string | null {
  const selectable = models.filter(isSelectableCodexModel)
  const ranked=selectable.filter(model=>typeof model.priority==='number'&&Number.isFinite(model.priority))
  if(ranked.length){
    const bestPriority=Math.min(...ranked.map(model=>model.priority as number))
    const best=ranked.filter(model=>model.priority===bestPriority)
    return best.find(model=>model.slug===preferredModel)?.slug??best[0].slug
  }
  // Compatibility for old downloaded manifests without a recommendation field.
  let best: { model: CodexCatalogModel; rank: CodexModelRank } | null = null
  for (const model of selectable) {
    const rank = rankCodexModel(model.slug)
    if (!rank) continue
    const difference = best ? compareCodexModelRank(rank, best.rank) : 1
    if (difference > 0 || (difference === 0 && model.slug === preferredModel)) {
      best = { model, rank }
    }
  }
  // Tier-first selection is a product preference, not a benchmark. Manifest priority
  // encodes administrator ordering; context size and reasoning levels do not rank intelligence.
  // Custom/non-OpenAI catalogs keep their existing preference/order without cross-provider claims.
  return best?.model.slug ?? selectable.find((model) => model.slug === preferredModel)?.slug ?? selectable[0]?.slug ?? null
}

export function selectCodexConfigReasoningEffort(
  model: CodexCatalogModel | undefined
): string | null {
  if (!model) return null
  const efforts = (model.supported_reasoning_levels ?? []).flatMap((level) => {
    const effort = trimEffort(level?.effort)
    return effort ? [effort] : []
  })
  if (efforts.length === 0) return null

  const defaultLevel = trimEffort(model.default_reasoning_level)
  if (defaultLevel && efforts.includes(defaultLevel)) {
    return defaultLevel === 'none' ? null : defaultLevel
  }
  return efforts.find((effort) => effort !== 'none') ?? null
}

export function formatCodexReasoningEffortTomlLine(effort: string | null): string {
  if (!effort) return ''
  return `model_reasoning_effort = "${effort}"\n`
}
