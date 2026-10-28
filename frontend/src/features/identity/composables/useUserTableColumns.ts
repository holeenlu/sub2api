import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/shared/ui/tableTypes'
import type { UserAttributeDefinition } from '../api/attributeTypes'

export interface UserTableColumnOptions {
  definitions(): UserAttributeDefinition[]
  values(): Record<number, Record<number, string>>
  onRevealData(): Promise<void>
  onReloadUsers(): Promise<void>
  onRevealGroups(): Promise<void>
}

/** Versioned column preferences and attribute presentation; queries remain in their request owner. */
export function useUserTableColumns(options: UserTableColumnOptions) {
  const { t } = useI18n()
  // Generate dynamic attribute columns from enabled definitions
  const attributeColumns = computed<Column[]>(() =>
    options.definitions()
      .filter(def => def.enabled)
      .map(def => ({
        key: `attr_${def.id}`,
        label: def.name,
        sortable: false
      }))
  )

  // Get formatted attribute value for display in table
  const getAttributeValue = (userId: number, attrId: number): string => {
    const userAttrs = options.values()[userId]
    if (!userAttrs) return '-'
    const value = userAttrs[attrId]
    if (!value) return '-'

    // Find definition for this attribute
    const def = options.definitions().find(d => d.id === attrId)
    if (!def) return value

    // Format based on type
    if (def.type === 'multi_select' && value) {
      try {
        const arr = JSON.parse(value)
        if (Array.isArray(arr)) {
          // Map values to labels
          return arr.map(v => {
            const opt = def.options?.find(o => o.value === v)
            return opt?.label || v
          }).join(', ')
        }
      } catch {
        return value
      }
    }

    if (def.type === 'select' && value && def.options) {
      const opt = def.options.find(o => o.value === value)
      return opt?.label || value
    }

    return value
  }

  // All possible columns (for column settings)
  const allColumns = computed<Column[]>(() => [
    { key: 'email', label: t('admin.users.columns.user'), sortable: true },
    { key: 'id', label: t('admin.users.columns.id'), sortable: true },
    { key: 'username', label: t('admin.users.columns.username'), sortable: true },
    { key: 'notes', label: t('admin.users.columns.notes'), sortable: false },
    // Dynamic attribute columns
    ...attributeColumns.value,
    { key: 'role', label: t('admin.users.columns.role'), sortable: true },
    { key: 'groups', label: t('admin.users.columns.groups'), sortable: false },
    { key: 'subscriptions', label: t('admin.users.columns.subscriptions'), sortable: false },
    { key: 'balance', label: t('admin.users.columns.balance'), sortable: true },
    { key: 'balance_platform_quota', label: t('admin.users.columns.balancePlatformQuota'), sortable: false },
    { key: 'usage', label: t('admin.users.columns.usage'), sortable: false },
    { key: 'usage_anthropic', label: t('admin.users.columns.usageAnthropic'), sortable: false },
    { key: 'usage_openai', label: t('admin.users.columns.usageOpenAI'), sortable: false },
    { key: 'usage_gemini', label: t('admin.users.columns.usageGemini'), sortable: false },
    { key: 'usage_antigravity', label: t('admin.users.columns.usageAntigravity'), sortable: false },
    { key: 'concurrency', label: t('admin.users.columns.concurrency'), sortable: true },
    { key: 'status', label: t('admin.users.columns.status'), sortable: true },
    { key: 'last_active_at', label: t('admin.users.columns.lastActive'), sortable: true },
    { key: 'last_used_at', label: t('admin.users.columns.lastUsed'), sortable: true },
    { key: 'created_by', label: t('admin.rolePermissions.createdBy'), sortable: false },
    { key: 'last_login_at', label: t('admin.rolePermissions.lastLogin'), sortable: false },
    { key: 'created_at', label: t('admin.users.columns.created'), sortable: true },
    { key: 'actions', label: t('admin.users.columns.actions'), sortable: false }
  ])

  // Columns that can be toggled (exclude email and actions which are always visible)
  const toggleableColumns = computed(() =>
    allColumns.value.filter(col => col.key !== 'email' && col.key !== 'actions')
  )

  // Hidden columns (stored in Set - columns NOT in this set are visible)
  // This way, new columns are visible by default
  const hiddenColumns = reactive<Set<string>>(new Set())

  // Default hidden columns (columns hidden by default on first load)
  const DEFAULT_HIDDEN_COLUMNS = [
    'notes', 'groups', 'subscriptions', 'usage', 'concurrency',
    'usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity',
    'balance_platform_quota'
  ]
  const REMOVED_COLUMNS = new Set(['last_login_at'])
  // 强制可见列：加载时会被强制移出 hiddenColumns，并在列设置 UI 上 disabled。
  // 当前没有列需要强制可见 —— last_active_at 已改为可被用户隐藏。
  const FORCED_VISIBLE_COLUMNS = new Set<string>()

  // localStorage keys for column settings
  const HIDDEN_COLUMNS_KEY = 'user-hidden-columns'
  // 列设置 schema 版本号。每次给 DEFAULT_HIDDEN_COLUMNS 新增列时 bump 一次，
  // 并在 VERSION_NEW_HIDDEN_COLUMNS 中登记该版本新增的 key。
  // 这样老用户升级后这些新列会被自动隐藏一次，而不会影响他们对其它老列的偏好。
  const COLUMN_SETTINGS_VERSION_KEY = 'user-column-settings-version'
  const COLUMN_SETTINGS_VERSION = 3
  const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
    2: ['usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity'],
    3: ['balance_platform_quota']
  }

  // Load saved column settings
  const loadSavedColumns = () => {
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
      if (saved) {
        const parsed = JSON.parse(saved) as string[]
        parsed
          .filter(key => !REMOVED_COLUMNS.has(key) && !FORCED_VISIBLE_COLUMNS.has(key))
          .forEach(key => hiddenColumns.add(key))

        // 老用户升级：把每个未应用过的版本里新增的默认隐藏列自动追加到 hiddenColumns。
        const storedVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
        if (storedVersion < COLUMN_SETTINGS_VERSION) {
          let mutated = false
          for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
            for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
              if (REMOVED_COLUMNS.has(key) || FORCED_VISIBLE_COLUMNS.has(key)) continue
              if (!hiddenColumns.has(key)) {
                hiddenColumns.add(key)
                mutated = true
              }
            }
          }
          if (mutated) saveColumnsToStorage()
          else localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
        }
      } else {
        // Use default hidden columns on first load
        DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
        localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      }
    } catch (e) {
      console.error('Failed to load saved columns:', e)
      DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
    }
  }

  // Save column settings to localStorage
  const saveColumnsToStorage = () => {
    try {
      localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
      localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    } catch (e) {
      console.error('Failed to save columns:', e)
    }
  }

  // Toggle column visibility
  const isForcedVisibleColumn = (key: string) => FORCED_VISIBLE_COLUMNS.has(key)
  const toggleColumn = (key: string) => {
    // 强制可见列(如 last_active_at)在加载时会被恢复成可见，
    // 这里阻止用户在当前会话隐藏它，避免"取消勾选 → 刷新又恢复"的反直觉行为。
    if (FORCED_VISIBLE_COLUMNS.has(key)) return
    const wasHidden = hiddenColumns.has(key)
    if (hiddenColumns.has(key)) {
      hiddenColumns.delete(key)
    } else {
      hiddenColumns.add(key)
    }
    saveColumnsToStorage()
    if (wasHidden && (key === 'usage' || key.startsWith('usage_') || key.startsWith('attr_') || key === 'balance_platform_quota')) {
      options.onRevealData()
    }
    if (key === 'subscriptions') {
      options.onReloadUsers()
    }
    if (wasHidden && key === 'groups') {
      options.onRevealGroups()
    }
  }

  // Check if column is visible (not in hidden set)
  const isColumnVisible = (key: string) => !hiddenColumns.has(key)
  // usage 主列或任意 usage_<platform> 子列可见时都需要批量拉取用量数据
  // 列 key → 平台名（'usage' 主列汇总所有平台时为 null）
  // 显式数组取代 Object.keys()：保证迭代顺序（决定列头排序按钮渲染顺序）
  // 不会因 JS 引擎差异或 USAGE_COLUMN_PLATFORMS 属性顺序调整而静默变化。
  const USAGE_COLUMN_KEYS: readonly string[] = ['usage', 'usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity']
  const USAGE_COLUMN_PLATFORMS: Record<string, string | null> = {
    usage: null,
    usage_anthropic: 'anthropic',
    usage_openai: 'openai',
    usage_gemini: 'gemini',
    usage_antigravity: 'antigravity'
  }
  const PLATFORM_USAGE_COLUMNS = USAGE_COLUMN_KEYS.filter((k) => k !== 'usage')
  const hasVisibleUsageColumn = computed(
    () => !hiddenColumns.has('usage') || PLATFORM_USAGE_COLUMNS.some((k) => !hiddenColumns.has(k))
  )
  const hasVisibleGroupsColumn = computed(() => !hiddenColumns.has('groups'))
  const hasVisiblePlatformQuotaColumn = computed(() => !hiddenColumns.has('balance_platform_quota'))
  const hasVisibleAttributeColumns = computed(() =>
    options.definitions().some((def) => def.enabled && !hiddenColumns.has(`attr_${def.id}`))
  )

  // Filtered columns based on visibility
  const columns = computed<Column[]>(() =>
    allColumns.value.filter(col =>
      col.key === 'email' || col.key === 'actions' || !hiddenColumns.has(col.key)
    )
  )



  return { getAttributeValue, toggleableColumns, loadSavedColumns, isForcedVisibleColumn, toggleColumn, isColumnVisible, USAGE_COLUMN_KEYS, USAGE_COLUMN_PLATFORMS, hasVisibleUsageColumn, hasVisibleGroupsColumn, hasVisiblePlatformQuotaColumn, hasVisibleAttributeColumns, columns }
}
