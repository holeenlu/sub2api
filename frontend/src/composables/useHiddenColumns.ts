import { reactive } from 'vue'

export interface HiddenColumnsOptions {
  /** 不可隐藏的列：toggle 对它们无效，isVisible 恒为 true */
  alwaysVisible?: string[]
  /** 首次使用（localStorage 里没有存档）时默认隐藏的列 */
  defaultHidden?: string[]
}

export interface HiddenColumns {
  /** 当前隐藏的列 key；响应式，可直接用在模板里 */
  hidden: Set<string>
  isVisible: (key: string) => boolean
  toggle: (key: string) => void
  /** 从 localStorage 读回存档；存档缺失或损坏时回落到 defaultHidden */
  load: () => void
  /** 按给定列顺序返回可见列的 key，方便直接传给子表格 */
  visibleKeys: (columns: ReadonlyArray<{ key: string }>) => string[]
}

/**
 * 表格列显隐的通用状态：一份隐藏集合 + localStorage 持久化。
 * 用量页各 tab 各自实例化一份（不同 storageKey），互不干扰。
 */
export function useHiddenColumns(storageKey: string, options: HiddenColumnsOptions = {}): HiddenColumns {
  const alwaysVisible = options.alwaysVisible ?? []
  const defaultHidden = options.defaultHidden ?? []
  const hidden = reactive(new Set<string>())

  const isVisible = (key: string) => alwaysVisible.includes(key) || !hidden.has(key)

  const persist = () => {
    try {
      localStorage.setItem(storageKey, JSON.stringify([...hidden]))
    } catch (e) {
      console.error(`Failed to save hidden columns (${storageKey}):`, e)
    }
  }

  const toggle = (key: string) => {
    if (alwaysVisible.includes(key)) return
    if (hidden.has(key)) {
      hidden.delete(key)
    } else {
      hidden.add(key)
    }
    persist()
  }

  const load = () => {
    hidden.clear()
    let keys: string[] = defaultHidden
    try {
      const saved = localStorage.getItem(storageKey)
      if (saved) {
        const parsed: unknown = JSON.parse(saved)
        // 存档损坏（非数组 / 混入非字符串）时按默认值处理，不让整页因一条坏记录挂掉
        keys = Array.isArray(parsed)
          ? parsed.filter((k): k is string => typeof k === 'string')
          : defaultHidden
      }
    } catch {
      keys = defaultHidden
    }
    keys.forEach((key) => {
      if (!alwaysVisible.includes(key)) hidden.add(key)
    })
  }

  const visibleKeys = (columns: ReadonlyArray<{ key: string }>) =>
    columns.filter((col) => isVisible(col.key)).map((col) => col.key)

  return { hidden, isVisible, toggle, load, visibleKeys }
}
