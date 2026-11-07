import { computed, ref, reactive, onMounted, onScopeDispose, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useHiddenColumns } from '@/shared/lib/useHiddenColumns'
import type { UsagePermissions } from '../services'
export type AdminUsageTab = 'usage' | 'errors' | 'ranking' | 'keyRanking'
export function useAdminUsageColumns(activeTab: Ref<AdminUsageTab>, permissions: Readonly<Ref<UsagePermissions>>) {
const { t } = useI18n()
const ALWAYS_VISIBLE = ['user', 'created_at']
const DEFAULT_HIDDEN_COLUMNS = ['reasoning_effort', 'request_id', 'upstream_request_id', 'user_agent']
const HIDDEN_COLUMNS_KEY = 'usage-hidden-columns'
const HIDDEN_COLUMNS_VERSION_KEY = 'usage-hidden-columns-version'
// 隐藏列版本链：每级只把当级新增列加入隐藏集，不重置用户已显式打开的列。
const HIDDEN_COLUMNS_PREV_VERSION = 'request-id-hidden-by-default'
const HIDDEN_COLUMNS_CURRENT_VERSION = 'upstream-request-id-hidden-by-default'

const allColumns = computed(() => [
  { key: 'user', label: t('admin.usage.user'), sortable: false },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  ...(permissions.value.isSuperAdmin ? [{ key: 'account', label: t('admin.usage.account'), sortable: false }] : []),
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'request_id', label: t('admin.usage.requestId'), sortable: false },
  { key: 'upstream_request_id', label: t('admin.usage.upstreamRequestId'), sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
  { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false }
])

const hiddenColumns = reactive<Set<string>>(new Set())

const toggleableColumns = computed(() =>
  allColumns.value.filter(col => !ALWAYS_VISIBLE.includes(col.key))
)

const visibleColumns = computed(() =>
  allColumns.value.filter(col =>
    ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key)
  )
)

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
    localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

// ---- 错误请求 tab 列设置(与用量明细同机制,独立存储) ----
const ERR_ALWAYS_VISIBLE = ['user', 'status', 'created_at', 'actions']
const ERR_DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const ERR_HIDDEN_COLUMNS_KEY = 'usage-error-hidden-columns'

// key 集合须与 OpsErrorLogTable 内部 allColumns 一致
const errAllColumns = computed(() => [
  { key: 'user', label: t('admin.ops.errorLog.user') },
  { key: 'api_key', label: t('admin.ops.errorLog.apiKey') },
  ...(permissions.value.isSuperAdmin ? [{ key: 'account', label: t('admin.ops.errorLog.account') }] : []),
  { key: 'platform', label: t('admin.ops.errorLog.platform') },
  { key: 'model', label: t('admin.ops.errorLog.model') },
  { key: 'endpoint', label: t('admin.ops.errorLog.endpoint') },
  { key: 'group', label: t('admin.ops.errorLog.group') },
  { key: 'type', label: t('admin.ops.errorLog.type') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('admin.ops.errorLog.status') },
  { key: 'message', label: t('admin.ops.errorLog.message') },
  { key: 'created_at', label: t('admin.ops.errorLog.time') },
  { key: 'user_agent', label: t('usage.userAgent') },
  { key: 'client_ip', label: t('admin.ops.errorLog.ip') },
  { key: 'actions', label: t('admin.ops.errorLog.action') },
])

const errorColumns = useHiddenColumns(ERR_HIDDEN_COLUMNS_KEY, {
  alwaysVisible: ERR_ALWAYS_VISIBLE,
  defaultHidden: ERR_DEFAULT_HIDDEN_COLUMNS,
})

const errToggleableColumns = computed(() =>
  errAllColumns.value.filter(col => !ERR_ALWAYS_VISIBLE.includes(col.key))
)

const errVisibleColumnKeys = computed(() => errorColumns.visibleKeys(errAllColumns.value))

// ---- API 密钥排行 tab 列设置(与上面两个同机制,独立存储) ----
// 密钥列是行的身份，隐藏后整行无意义，因此不可切换。
const KEY_RANKING_ALWAYS_VISIBLE = ['key']

// key 集合须与 APIKeyTokenRanking / BreakdownRanking 内部的列定义一致
const keyRankingAllColumns = computed(() => [
  { key: 'key', label: t('admin.usage.keyRanking.columns.key') },
  { key: 'user', label: t('admin.usage.keyRanking.columns.user') },
  { key: 'requests', label: t('admin.usage.tokenRanking.columns.requests') },
  { key: 'input_tokens', label: t('admin.usage.tokenRanking.columns.inputTokens') },
  { key: 'output_tokens', label: t('admin.usage.tokenRanking.columns.outputTokens') },
  { key: 'cache_tokens', label: t('admin.usage.tokenRanking.columns.cacheTokens') },
  { key: 'total_tokens', label: t('admin.usage.tokenRanking.columns.totalTokens') },
  { key: 'actual_cost', label: t('admin.usage.tokenRanking.columns.cost') },
])

const keyRankingColumns = useHiddenColumns('usage-key-ranking-hidden-columns', {
  alwaysVisible: KEY_RANKING_ALWAYS_VISIBLE,
})
const keyRankingToggleableColumns = computed(() =>
  keyRankingAllColumns.value.filter(col => !KEY_RANKING_ALWAYS_VISIBLE.includes(col.key))
)
const keyRankingVisibleColumnKeys = computed(() => keyRankingColumns.visibleKeys(keyRankingAllColumns.value))

// 列设置下拉按当前 tab 分发。用户排行没有可切换的列，按钮对它隐藏。
const hasColumnSettings = computed(() => activeTab.value !== 'ranking')
const currentToggleableColumns = computed(() => {
  if (activeTab.value === 'errors') return errToggleableColumns.value
  if (activeTab.value === 'keyRanking') return keyRankingToggleableColumns.value
  return toggleableColumns.value
})
const isCurrentColumnVisible = (key: string) => {
  if (activeTab.value === 'errors') return errorColumns.isVisible(key)
  if (activeTab.value === 'keyRanking') return keyRankingColumns.isVisible(key)
  return isColumnVisible(key)
}
const toggleCurrentColumn = (key: string) => {
  if (activeTab.value === 'errors') return errorColumns.toggle(key)
  if (activeTab.value === 'keyRanking') return keyRankingColumns.toggle(key)
  return toggleColumn(key)
}

const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      (JSON.parse(saved) as string[]).forEach((key) => {
        hiddenColumns.add(key)
      })
      const savedVersion = localStorage.getItem(HIDDEN_COLUMNS_VERSION_KEY)
      if (savedVersion !== HIDDEN_COLUMNS_CURRENT_VERSION) {
        if (savedVersion !== HIDDEN_COLUMNS_PREV_VERSION) {
          hiddenColumns.add('request_id')
        }
        hiddenColumns.add('upstream_request_id')
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
        localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        hiddenColumns.add(key)
      })
      localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
    }
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
      hiddenColumns.add(key)
    })
  }
}

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}


onMounted(() => { loadSavedColumns(); errorColumns.load(); keyRankingColumns.load(); document.addEventListener('click', handleColumnClickOutside) })
onScopeDispose(() => document.removeEventListener('click', handleColumnClickOutside))
return { visibleColumns, errVisibleColumnKeys, keyRankingVisibleColumnKeys, hasColumnSettings, currentToggleableColumns, isCurrentColumnVisible, toggleCurrentColumn, showColumnDropdown, columnDropdownRef }
}
