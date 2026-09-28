export interface ModelAllowlistConfig {
  enabled: boolean
  models: string[]
}

export interface ModelAllowlistItem {
  id: string
  selected: boolean
}

export interface ModelAllowlistState {
  enabled: boolean
  savedModels: string[]
  items: ModelAllowlistItem[]
}

// 自定义条目校验错误码，由视图映射为 i18n 提示。
export type ModelAllowlistAddError = 'empty' | 'duplicate'

export const createModelAllowlistState = (
  config?: Partial<ModelAllowlistConfig> | null,
): ModelAllowlistState => ({
  enabled: config?.enabled ?? false,
  savedModels: normalizeModels(config?.models ?? []),
  items: [],
})

export const hydrateModelAllowlistState = (
  config: Partial<ModelAllowlistConfig> | null | undefined,
  candidates: string[],
): ModelAllowlistState => {
  const state = createModelAllowlistState(config)
  setModelAllowlistCandidates(state, candidates)
  return state
}

/**
 * 候选列表只做「补充」，不做「裁剪」。
 *
 * 候选的三个来源（平台默认模型、账号 model_mapping 的 key、上游 /v1/models）
 * 都不是分组 model_allowlist 的全集：网关对 anthropic 的允许集就是 mapping key ∪
 * 默认列表，上游列表里也不会有已下架的旧模型，偶发少返回一个同样常见。按候选
 * 反向裁剪已保存项，会让管理员打开编辑弹窗、什么都不改直接保存，就把这些条目
 * 永久删掉。
 */
export const setModelAllowlistCandidates = (
  state: ModelAllowlistState,
  candidates: string[],
) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => item.id),
  )
  const currentKnown = new Set(state.items.map(item => item.id))
  const savedSelected = new Set(state.savedModels)
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([
    ...state.items.map(item => item.id),
    ...state.savedModels,
    ...normalizedCandidates,
  ])

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(id)
      : state.savedModels.length > 0
        ? savedSelected.has(id)
        : normalizedCandidates.includes(id)

    return {
      id,
      selected: selected && (currentKnown.has(id) || savedSelected.has(id) || state.savedModels.length === 0),
    }
  })
}

export const toggleModelAllowlistItem = (
  state: ModelAllowlistState,
  modelID: string,
) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

export const selectAllModelAllowlistItems = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = true
  })
}

export const invertModelAllowlistSelection = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = !item.selected
  })
}

export const moveModelAllowlistItem = (
  state: ModelAllowlistState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

// addCustomModelAllowlistItem 把手工输入的条目追加到白名单末尾（选中状态）。
// 去重；`*` 可出现在任意位置。返回错误码或 null（成功）。
export const addCustomModelAllowlistItem = (
  state: ModelAllowlistState,
  raw: string,
): ModelAllowlistAddError | null => {
  const entry = raw.trim()
  if (!entry) {
    return 'empty'
  }
  if (
    state.items.some(item => item.id.toLowerCase() === entry.toLowerCase()) ||
    state.savedModels.some(model => model.toLowerCase() === entry.toLowerCase())
  ) {
    return 'duplicate'
  }
  state.items.push({ id: entry, selected: true })
  return null
}

export const buildModelAllowlistConfig = (
  state: ModelAllowlistState,
): ModelAllowlistConfig => ({
  enabled: state.enabled,
  models: state.items.length > 0
    ? state.items.filter(item => item.selected).map(item => item.id)
    : [...state.savedModels],
})

export const selectedModelAllowlistCount = (state: ModelAllowlistState): number =>
  state.items.filter(item => item.selected).length

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    out.push(model)
  }
  return out
}
