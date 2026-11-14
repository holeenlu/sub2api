import { computed, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/shared/ui/tableTypes'

export function useKeyColumns() {
  const { t } = useI18n()
  const allColumns = computed<Column[]>(() => [
    { key: 'name', label: t('common.name'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'id', label: t('keys.id'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'key', label: t('keys.apiKey'), sortable: false, class: 'whitespace-nowrap' },
    { key: 'group', label: t('keys.group'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'current_concurrency', label: t('keys.currentConcurrency'), sortable: true, class: 'min-w-[8rem] whitespace-nowrap' },
    { key: 'usage', label: t('keys.usage'), sortable: false, class: 'whitespace-nowrap' },
    { key: 'rate_limit', label: t('keys.rateLimitColumn'), sortable: false, class: 'whitespace-nowrap' },
    { key: 'expires_at', label: t('keys.expiresAt'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'status', label: t('common.status'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'last_used_at', label: t('keys.lastUsedAt'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'last_used_ip', label: t('keys.lastUsedIP'), sortable: false, class: 'whitespace-nowrap' },
    { key: 'created_at', label: t('keys.created'), sortable: true, class: 'whitespace-nowrap' },
    { key: 'actions', label: t('common.actions'), sortable: false, class: 'whitespace-nowrap' }
  ])

  const ALWAYS_VISIBLE_COLUMNS = new Set(['name', 'actions'])
  const DEFAULT_HIDDEN_COLUMNS = ['id', 'rate_limit', 'last_used_at', 'last_used_ip']
  const HIDDEN_COLUMNS_KEY = 'api-key-hidden-columns'
  const COLUMN_SETTINGS_VERSION_KEY = 'api-key-column-settings-version'
  const COLUMN_SETTINGS_VERSION = 3
  const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
    2: ['last_used_ip'],
    3: ['id']
  }

  const toggleableColumns = computed(() =>
    allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key))
  )

  const hiddenColumns = reactive<Set<string>>(new Set())

  const saveColumnsToStorage = () => {
    try {
      localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
      localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    } catch (error) {
      console.error('Failed to save API key table columns:', error)
    }
  }

  const loadSavedColumns = () => {
    hiddenColumns.clear()
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
      if (saved) {
        const parsed = JSON.parse(saved) as string[]
        const validColumnKeys = new Set(allColumns.value.map((col) => col.key))
        parsed
          .filter((key) =>
            typeof key === 'string' &&
            validColumnKeys.has(key) &&
            !ALWAYS_VISIBLE_COLUMNS.has(key)
          )
          .forEach((key) => hiddenColumns.add(key))
        const storedVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
        if (storedVersion < COLUMN_SETTINGS_VERSION) {
          for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
            for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
              if (validColumnKeys.has(key) && !ALWAYS_VISIBLE_COLUMNS.has(key)) {
                hiddenColumns.add(key)
              }
            }
          }
          saveColumnsToStorage()
        } else {
          localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
        }
      } else {
        DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
        localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      }
    } catch (error) {
      console.error('Failed to load API key table columns:', error)
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
    }
  }

  const toggleColumn = (key: string) => {
    if (ALWAYS_VISIBLE_COLUMNS.has(key)) return
    if (hiddenColumns.has(key)) {
      hiddenColumns.delete(key)
    } else {
      hiddenColumns.add(key)
    }
    saveColumnsToStorage()
  }

  const isColumnVisible = (key: string) => !hiddenColumns.has(key)

  const columns = computed<Column[]>(() =>
    allColumns.value.filter((col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key))
  )


  onMounted(loadSavedColumns)
  return { columns, toggleableColumns, toggleColumn, isColumnVisible }
}
