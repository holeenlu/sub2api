<script setup lang="ts">
import { computed, nextTick, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ActionMenu from './ActionMenu.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import AccountStatusIndicator from '@/components/account/AccountStatusIndicator.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { useIntervalFn, useNow } from '@vueuse/core'
import { formatCountdownWithSuffix, formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpCancelled, StepUpCancelledError } from '@/composables/useStepUp'
import { useClipboard } from '@/composables/useClipboard'
import { resourceAPI, vendorAPI } from '../api'
import {
  accountQuotaRows,
  display,
  displayPercent,
  formatUsageCost,
  formatUsageNumber,
  download,
  errorText,
  resourceOwnerScope,
  selectionForPage,
  accountOpenAIModes,
  openAIAuthorizationModes,
  vendorOpenAIModes
} from '../helpers'
import { useVendorPage } from '../useVendorPage'
import type { AccountAuthorizationPatch, OpenAIAuthorizationMode, AccountPage, AccountOverview, Vendor, VendorAccount, VendorGroup, ResourceScope } from '../types'
import RequestState from './RequestState.vue'
import AccountForm from './AccountForm.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import BulkEditAccountModal from '@/components/account/BulkEditAccountModal.vue'
import { apiClient } from '@/api/client'
import { adminAPI } from '@/api/admin'
import type { Account, AdminGroup, Proxy, AccountType } from '@/types'
import AccountAuthorizations from './AccountAuthorizations.vue'
import AccountImport from './AccountImport.vue'
import UsageWorkbench from './UsageWorkbench.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { useHiddenColumns } from '@/composables/useHiddenColumns'
import AccountColumnChooser from './AccountColumnChooser.vue'
import AccountCreatedRangePicker from './AccountCreatedRangePicker.vue'
const props = defineProps<{ scope: ResourceScope; vendor?: Vendor; groups?: VendorGroup[] }>()
const emit = defineEmits<{ selectVendor: [vendorId: number] }>()
const readOnly = computed(() => props.scope.readOnly === true)
const showOwner = computed(() => props.scope.admin || readOnly.value)
const availableGroups = computed(() => props.vendor?.groups ?? props.groups ?? [])
// Account DTO groups may contain only id/name; resolve colour from known metadata.
function accountGroupPlatform(group: VendorGroup, account: VendorAccount) {
  return group.platform || availableGroups.value.find(item => item.id === group.id)?.platform || account.platform
}
const usageVendorId = computed(() => usageAccount.value?.vendor_id ?? props.scope.vendorId)
const { t, te } = useI18n(),
  stepUp = useStepUp()
const { copyToClipboard } = useClipboard()
const translated = (key: string) => (te('vendor.' + key) ? t('vendor.' + key) : t('vendor.unknown'))
// Keep native account health separate from supplier policy/supply blockers.
const statusAccount = (account: VendorAccount) => ({
  ...account, schedulable: account.platform_schedulable ?? account.schedulable
})
const statusLabel = (value?: string) => translated(value || 'unknown')
const platformLabel = (value?: string) => platformDisplayName(value) || t('vendor.unknown')
const blockerLabel = (value: string) => translated('accountBlockers.' + value)
const detailValue = (value: unknown) =>
  value === null || value === undefined || value === '' ? t('vendor.unknown') : display(value)
const detailDate = (value?: string | null) => formatDateTime(value) || detailValue(value)
function methodLabel(value?: string) {
  const labels: Record<string, string> = {
    oauth: t('vendor.authMethods.oauth', 'OAuth'),
    'setup-token': t('vendor.authMethods.oauthToken', 'OAuth Token'),
    'setup-token-manual': t('vendor.authMethods.setupToken', 'Setup Token'),
    refresh_token: t('vendor.authMethods.refreshToken', 'Refresh Token')
  }
  return labels[value || ''] || t('vendor.unknown')
}
function accountBadges(account: VendorAccount) {
  const modes = accountOpenAIModes(account)
  return [
    account.plan_type,
    account.subscription_type,
    methodLabel(account.method || account.type),
    modes.length ? t('vendor.authorization.' + (modes.length === 2 ? 'dual' : modes[0])) : ''
  ].filter((value): value is string => Boolean(value && value !== t('vendor.unknown')))
}
function accountOperations(account: VendorAccount) {
  return ([['refreshToken', 'refresh'], ['refreshQuota', 'usage-refresh'], ['probe', 'probe'], ['sync', 'sync']] as const)
    .filter(([, operation]) => account.platform !== 'openai' || operation !== 'refresh')
}
function metricValue(value: unknown) {
  return value === null || value === undefined ? '—' : formatUsageNumber(value)
}
function metricPercent(value: number | null | undefined) {
  return value == null ? '—' : `${(value * 100).toFixed(2)}%`
}
const clock = useNow({ interval: 30000 })
function quotaCountdown(value: unknown) {
  void clock.value
  return typeof value === 'string' ? formatCountdownWithSuffix(value) : null
}
const overview = ref<AccountOverview>()
const filters = reactive({
    search: '',
    platform: '',
    status: '',
    group_id: '',
    method: '',
    authorization_mode: '',
    failure_category: '',
    cooldown: '',
    schedulability: '',
    created_from: '',
    created_to: '',
    sort_by: 'created_at',
    sort_order: 'desc'
  }),
  applied = ref({ ...filters })
const overviewOpen = ref(true)
const filtersOpen = ref(true)
const accountColumns = [
  ['name', 'name'],
  ['status', 'status'],
  ['groups', 'groups'],
  ['quota', 'quotaWindows'],
  ['concurrency', 'concurrency'],
  ['priority', 'priority'],
  ['today', 'todayCost'],
  ['total', 'allTimeCost'],
  ['notes', 'notes'],
  ['cache', 'cacheHit'],
  ['created', 'createdAt']
] as const
const hiddenColumns = useHiddenColumns('vendor-account-columns', { alwaysVisible: ['name', 'status'] })
hiddenColumns.load()
const isColumnVisible = (key: string) => hiddenColumns.isVisible(key)
const columnOptions = computed(() =>
  accountColumns.map(([key, label]) => ({ key, label: t('vendor.' + label) }))
)
const failureCategories = computed(() => [
  ...new Set([
    'authentication',
    'kyc',
    'terms',
    'tier',
    'duplicate',
    'email_mismatch',
    'rejected',
    'connection',
    'permission',
    'proxy',
    'upstream',
    'unknown',
    ...Object.keys(overview.value?.failures || {})
  ])
])
const distributionKeys = ['normal', 'near_limit', 'full', 'unknown'] as const
const distributionColors = {
  normal: 'bg-emerald-500/60',
  near_limit: 'bg-amber-500/70',
  full: 'bg-red-500/70',
  unknown: 'bg-gray-300'
}
function distributionPercent(distribution: Record<string, number>, key: string) {
  const total = Object.values(distribution).reduce((sum, count) => sum + count, 0)
  return total ? (distribution[key] / total) * 100 : 0
}
function rowQuotas(account: VendorAccount) {
  const snapshots = accountQuotaRows(account.quota)
  return (
    account.platform === 'anthropic'
      ? ['seven_day_fable', 'seven_day', 'five_hour']
      : ['seven_day', 'five_hour']
  ).map(
    (name) =>
      snapshots.find((row) => row.name === name) || { name, utilization: null, resets_at: null }
  )
}
function shortReset(value: unknown) {
  if (typeof value !== 'string' || !Number.isFinite(new Date(value).getTime()))
    return t('vendor.resetUnknown')
  return formatDateTime(value).slice(5, 16)
}
const sortChoice = computed({
  get: () => filters.sort_by + ':' + filters.sort_order,
  set: (value: string) => {
    const [key, order] = value.split(':')
    filters.sort_by = key
    filters.sort_order = order
  }
})
const sortOptions = computed(() =>
  [
    ['created_at', 'createdAt'],
    ['today_cost', 'todayCost'],
    ['total_cost', 'allTimeCost'],
    ['fable_cost', 'fableCost'],
    ['opus_cost', 'opusCost'],
    ['name', 'name']
  ].flatMap(([key, label]) =>
    ['desc', 'asc'].map((order) => ({
      value: key + ':' + order,
      label: t('vendor.' + label) + (order === 'desc' ? ' ↓' : ' ↑')
    }))
  )
)
function resetFilters() {
  Object.assign(filters, {
    search: '',
    platform: '',
    status: '',
    group_id: '',
    method: '',
    authorization_mode: '',
    failure_category: '',
    cooldown: '',
    schedulability: '',
    created_from: '',
    created_to: '',
    sort_by: 'created_at',
    sort_order: 'desc'
  })
  load(true)
}
function applyCreatedRange(range: { start: string; end: string }) {
  filters.created_from = range.start
  filters.created_to = range.end
  load(true)
}
const list = useVendorPage<VendorAccount>((q, s) => resourceAPI(props.scope).accounts(q, s))
const { items, page, pageSize, total, loading, error, selected } = list
const form = ref(false),
  editing = ref<VendorAccount>(),
  reauthorize = ref(false),
  authorizationMode = ref<OpenAIAuthorizationMode>(),
  returnDetail = ref<VendorAccount>(),
  revokeTarget = ref<{ account: VendorAccount; mode: OpenAIAuthorizationMode }>(),
  detail = ref<VendorAccount>(),
  usageId = ref<number>(),
  usageAccount = ref<VendorAccount>(),
  usageReturnDetail = ref<VendorAccount>(),
  importing = ref(false),
  exporting = ref(false),
  busy = ref(false),
  actionError = ref(''),
  actionResult = ref<{
    label: string
    status: string
    latency?: unknown
    collected?: unknown
  }>(),
  confirmDelete = ref<VendorAccount>(),
  confirmExport = ref(false)
const managementAccount = ref<Account | null>(null)
const managementGroups = ref<AdminGroup[]>([])
const managementProxies = ref<Proxy[]>([])
const managementVendorId = ref<number>()
const bulkEditing = ref(false)
const selectionRows = new Map<number, VendorAccount>()
watch([items, selected], () => {
  for (const row of items.value) if (selected.value.has(row.id)) selectionRows.set(row.id, row)
  for (const id of selectionRows.keys()) if (!selected.value.has(id)) selectionRows.delete(id)
}, { deep: true })
const selectedRows = computed(() => [...selected.value].map(id => selectionRows.get(id) ?? items.value.find(row => row.id === id)).filter((row): row is VendorAccount => !!row && !row.archived))
async function managementOptions(rows: VendorAccount[]) {
  const owners = [...new Set(rows.map(row => row.vendor_id))]
  const policies = await Promise.all(owners.map(async id => (await apiClient.get<Vendor>('/admin/vendors/' + id)).data))
  const groups = await adminAPI.groups.getAll()
  managementGroups.value = groups.filter(group => policies.every(policy => policy.group_ids.includes(group.id)))
  managementProxies.value = []
  if (owners.length === 1) {
    let page = 1, total = 0
    do {
      const result = await resourceAPI({ admin: true, vendorId: owners[0] }).proxies({ page, page_size: 50 })
      managementProxies.value.push(...result.items as unknown as Proxy[])
      total = result.total; page++
      if (!result.items.length) break
    } while (managementProxies.value.length < total)
  }
}
async function loadManagementAccount(row: VendorAccount): Promise<Account> {
  return (await apiClient.get<Account>('/admin/accounts/' + row.id, { params: { vendor_id: row.vendor_id, view: 'management' } })).data
}
async function openManagementAccount(row: VendorAccount) {
  if (!props.scope.admin || row.archived) return
  busy.value = true
  const epoch = scopeEpoch
  try {
    const account = await loadManagementAccount(row)
    await managementOptions([row])
    if (epoch !== scopeEpoch) return
    managementVendorId.value = row.vendor_id
    managementAccount.value = account
  } catch (e) { actionError.value = errorText(e) }
  finally { if (epoch === scopeEpoch) busy.value = false }
}
async function saveManagementAccount(id: number, body: Record<string, unknown>): Promise<Account> {
  return (await apiClient.put<Account>('/admin/accounts/' + id, body, { params: { vendor_id: managementVendorId.value, view: 'management' } })).data
}
async function openBulkEditor() {
  if (!props.scope.admin || !selectedRows.value.length) return
  busy.value = true
  const epoch = scopeEpoch
  try {
    await managementOptions(selectedRows.value)
    if (epoch === scopeEpoch) bulkEditing.value = true
  } catch (e) { actionError.value = errorText(e) }
  finally { if (epoch === scopeEpoch) busy.value = false }
}
const saveManagementBatch = async (ids: number[], body: Record<string, unknown>) =>
  (await apiClient.post<Awaited<ReturnType<typeof adminAPI.accounts.bulkUpdate>>>('/admin/accounts/bulk-update',
    { account_ids: ids, ...body }, { params: { vendor_id: props.scope.vendorId ?? 0, view: 'management' } })).data
const selectableAccounts = computed(() => items.value.filter((account) => !account.archived))
const allSelected = computed(
  () =>
    selectableAccounts.value.length > 0 &&
    selectableAccounts.value.every((a) => selected.value.has(a.id))
)
let scopeEpoch = 0
let detailOpener: HTMLElement | undefined
let usageOpener: HTMLElement | undefined
const usageDetailOpener = ref<HTMLButtonElement>()
async function load(reset = false, background = false) {
  const epoch = scopeEpoch
  if (reset) applied.value = { ...filters }
  const result = (await list.load(applied.value, reset, background)) as AccountPage | undefined
  if (result && epoch === scopeEpoch) {
    overview.value = result.overview
    for (const account of result.items) {
      if (account.archived) selected.value.delete(account.id)
    }
  }
}
const refreshInterval = ref('60')
useIntervalFn(
  () => {
    if (
      refreshInterval.value === '0' ||
      document.hidden ||
      loading.value ||
      busy.value ||
      form.value ||
      importing.value ||
      exporting.value ||
      detail.value ||
      usageId.value ||
      confirmDelete.value ||
      confirmExport.value ||
      selected.value.size
    )
      return
    void load(false, true)
  },
  computed(() => Math.max(60, Number(refreshInterval.value)) * 1000)
)
watch(
  () => [props.scope.admin, props.scope.vendorId, props.scope.readOnly, props.scope.children],
  () => {
    scopeEpoch++
    selected.value = new Set()
    managementAccount.value = null
    bulkEditing.value = false
    selectionRows.clear()
    detail.value = undefined
    detailOpener = undefined
    editing.value = undefined
    form.value = false
    reauthorize.value = false
    authorizationMode.value = undefined
    returnDetail.value = undefined
    revokeTarget.value = undefined
    importing.value = false
    exporting.value = false
    confirmExport.value = false
    stepUp.onCancel()
    confirmDelete.value = undefined
    usageId.value = undefined
    usageAccount.value = undefined
    usageOpener = undefined
    usageReturnDetail.value = undefined
    busy.value = false
    overview.value = undefined
    total.value = 0
    actionError.value = ''
    actionResult.value = undefined
    load(true)
  },
  { immediate: true, flush: 'sync' }
)
onUnmounted(() => {
  scopeEpoch++
  stepUp.onCancel()
})
function openForm(account?: VendorAccount, reauth = false, mode?: OpenAIAuthorizationMode) {
  if (props.scope.admin && account && !reauth) { void openManagementAccount(account); return }
  if (readOnly.value || account?.archived) return
  editing.value = account
  reauthorize.value = reauth
  authorizationMode.value = mode
  if (account && detail.value?.id === account.id) {
    returnDetail.value = detail.value
    detail.value = undefined
  }
  form.value = true
}
async function closeForm(saved = false) {
  const target = returnDetail.value, epoch = scopeEpoch
  form.value = false
  returnDetail.value = undefined
  if (saved) await load()
  if (target && epoch === scopeEpoch) await showDetail(target)
}
async function changeAuthorization(patch?: AccountAuthorizationPatch) {
  const account = patch ? detail.value : revokeTarget.value?.account
  const mode = patch ? (patch.native_enabled !== undefined ? 'native' : 'bps') : revokeTarget.value?.mode
  if (!account || !mode || busy.value || readOnly.value || account.archived || !vendorOpenAIModes(props.vendor, account).includes(mode)) return
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  actionResult.value = undefined
  try {
    const api = resourceAPI(props.scope)
    if (patch) await api.updateAccount(account.id, patch)
    else await api.revokeAuthorization(account.id, mode)
    if (epoch !== scopeEpoch) return
    revokeTarget.value = undefined
    await load()
    if (epoch === scopeEpoch && detail.value?.id === account.id) await showDetail(account)
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
function openImport() {
  if (readOnly.value) return
  importing.value = true
}
function openExport(account?: VendorAccount) {
  if (readOnly.value || account?.archived) return
  if (account) selected.value = new Set([account.id])
  confirmExport.value = true
}
function openDelete(account: VendorAccount) {
  if (readOnly.value || account.archived) return
  confirmDelete.value = account
}
async function showDetail(account: VendorAccount, event?: Event) {
  if (event?.currentTarget instanceof HTMLElement) detailOpener = event.currentTarget
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    const accountDetail = await resourceAPI(resourceOwnerScope(props.scope, account.vendor_id)).account(account.id)
    if (epoch === scopeEpoch) detail.value = accountDetail
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
function openUsage(account: VendorAccount, event?: Event) {
  usageOpener = event?.currentTarget instanceof HTMLElement ? event.currentTarget : undefined
  usageAccount.value = account
  usageReturnDetail.value = detail.value
  detail.value = undefined
  usageId.value = account.id
}
async function closeUsage() {
  const epoch = scopeEpoch
  const opener = usageOpener
  usageOpener = undefined
  usageId.value = undefined
  usageAccount.value = undefined
  detail.value = usageReturnDetail.value
  usageReturnDetail.value = undefined
  // Let the newly mounted dialog set its initial focus before returning to the usage opener.
  await nextTick()
  await nextTick()
  if (epoch !== scopeEpoch) return
  if (detail.value) usageDetailOpener.value?.focus({ preventScroll: true })
  else if (opener?.isConnected) opener.focus({ preventScroll: true })
}
async function closeDetail() {
  const opener = detailOpener
  const epoch = scopeEpoch
  detail.value = undefined
  detailOpener = undefined
  await nextTick()
  if (epoch === scopeEpoch && !detail.value && opener?.isConnected) {
    opener.focus({ preventScroll: true })
  }
}
async function action(
  account: VendorAccount,
  operation: 'pause' | 'resume' | 'refresh' | 'usage-refresh' | 'probe' | 'sync',
  mode?: OpenAIAuthorizationMode
) {
  if (readOnly.value || account.archived) return
  if (mode && (!vendorOpenAIModes(props.vendor, account).includes(mode) || !account.authorizations?.find(grant => grant.mode === mode)?.can_refresh)) return
  if (operation === 'probe' && !window.confirm(t('vendor.probeWarning'))) return
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    actionResult.value = undefined
    const api = resourceAPI(props.scope)
    const result = mode ? await api.accountAction(account.id, operation, mode) : await api.accountAction(account.id, operation)
    if (epoch !== scopeEpoch) return
    actionResult.value = {
      label: {
        pause: 'pause',
        resume: 'resume',
        refresh: 'refreshToken',
        'usage-refresh': 'refreshQuota',
        probe: 'probe',
        sync: 'sync'
      }[operation],
      status: operation === 'probe' ? String(result.status || 'unknown') : 'success',
      latency: result.latency_ms,
      collected: result.collected_at
    }
    await load()
    if (epoch === scopeEpoch && detail.value?.id === account.id) await showDetail(account)
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function remove() {
  const account = confirmDelete.value
  if (readOnly.value || !account || account.archived || busy.value) return
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    await resourceAPI(props.scope).deleteAccount(account.id)
    if (epoch !== scopeEpoch) return
    selected.value.delete(account.id)
    if (confirmDelete.value?.id === account.id) confirmDelete.value = undefined
    if (detail.value?.id === account.id) detail.value = undefined
    await load()
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function exportSelected() {
  if (readOnly.value || exporting.value || !selected.value.size) return
  const accounts = detail.value ? [...items.value, detail.value] : items.value
  if (accounts.some((account) => account.archived && selected.value.has(account.id))) {
    actionError.value = blockerLabel('archived')
    return
  }
  exporting.value = true
  actionError.value = ''
  const epoch = scopeEpoch,
    scope = { ...props.scope }
  try {
    const ids = [...selected.value]
    const data = await stepUp.run(async () => {
      if (epoch !== scopeEpoch || readOnly.value) throw new StepUpCancelledError()
      try {
        return await vendorAPI.exportAccounts(ids, scope)
      } catch (e) {
        // A late challenge must not reopen verification in a different scope.
        if (epoch !== scopeEpoch || readOnly.value) throw new StepUpCancelledError()
        throw e
      }
    })
    if (epoch !== scopeEpoch || readOnly.value) return
    download(data, 'supplier-authorization.json')
    confirmExport.value = false
  } catch (e) {
    if (epoch === scopeEpoch && !isStepUpCancelled(e)) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) exporting.value = false
  }
}
const quotaRows = computed(() => (detail.value ? rowQuotas(detail.value) : []))
const blockers = computed(() => [...new Set(detail.value?.blockers || [])])
const cooldownFields = computed(() => ([
  ['resetAt', detail.value?.rate_limit_reset_at],
  ['overloadUntil', detail.value?.overload_until],
  ['tempUnschedulableUntil', detail.value?.temp_unschedulable_until]
] as const))
const activeCooldowns = computed(() => cooldownFields.value.filter((entry): entry is [typeof entry[0], string] =>
  typeof entry[1] === 'string' && Date.parse(entry[1]) > clock.value.getTime()))
const cooldownKnown = computed(() => cooldownFields.value.every(([, value]) => value === null || (typeof value === 'string' && Number.isFinite(Date.parse(value)))))
const safeProxyEndpoint = computed(() => {
  if (!detail.value?.proxy_id || !detail.value.proxy?.endpoint) return ''
  try {
    const endpoint = new URL(detail.value.proxy.endpoint)
    if (!['http:', 'https:', 'socks5:', 'socks5h:'].includes(endpoint.protocol) || !endpoint.hostname) return ''
    // Copy only the endpoint, even if an unexpected payload includes URL credentials.
    return endpoint.protocol + '//' + endpoint.host
  } catch {
    return ''
  }
})
const overviewTitle = computed(() => props.scope.admin || readOnly.value
  ? (props.vendor?.name || t(props.scope.children ? 'vendor.allChildren' : 'vendor.allVendors')) + ' · ' + t('vendor.accounts')
  : t('vendor.accountOverview'))
function copyProxyEndpoint() {
  if (readOnly.value || !safeProxyEndpoint.value) return
  void copyToClipboard(safeProxyEndpoint.value)
}
</script>
<template>
  <section class="space-y-4">
    <div class="flex min-h-8 flex-wrap items-center justify-between gap-3">
      <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 class="text-2xl font-semibold">{{ t('vendor.accounts') }}</h1>
        <p class="text-xs text-gray-500">
          {{ scope.admin || readOnly ? (vendor?.name || t(scope.children ? 'vendor.allChildren' : 'vendor.allVendors')) : t('vendor.ownResources') }} · RPM {{ display(overview?.rpm) }} · TPM
          {{ display(overview?.tpm_with_cache) }} · {{ t('vendor.liveConcurrency') }}
          {{ display(overview?.current_concurrency) }} /
          {{ display(overview?.maximum_concurrency) }} · {{ t('vendor.accountCountTotal') }}
          {{ display(overview?.total) }}<template v-if="vendor"> / {{ vendor.account_limit === 0 ? t('vendor.unlimited') : vendor.account_limit }}</template>
        </p>
      </div>
      <div class="flex items-center gap-2">
        <button class="btn btn-secondary btn-sm" :disabled="loading" @click="load()">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />{{
            t('vendor.refresh')
          }}
        </button>
        <button v-if="scope.admin" class="btn btn-secondary btn-sm" data-testid="vendor-bulk-edit"
          :disabled="!selectedRows.length || busy" @click="openBulkEditor">{{ t('admin.accounts.bulkEdit.title') }}</button>
        <ActionMenu v-if="!readOnly" :label="t('vendor.moreActions')"
          ><button role="menuitem" @click="openImport">
            <Icon name="upload" size="sm" />{{ t('vendor.import') }}</button
          ><button
            role="menuitem"
            :aria-label="t('vendor.export')"
            :disabled="!selected.size || exporting"
            @click="openExport()"
          >
            <Icon name="download" size="sm" />{{ t('vendor.export')
            }}<span class="ml-auto text-xs text-gray-400">{{ selected.size }}</span>
          </button></ActionMenu
        >
        <button v-if="!readOnly" class="btn btn-primary btn-sm" @click="openForm()">
          <Icon name="plus" size="sm" />{{ t('vendor.addAccount') }}
        </button>
      </div>
    </div>
    <section class="card overflow-hidden" :aria-label="overviewTitle">
      <div
        class="vendor-filter-bar flex min-h-[52px] flex-wrap items-center justify-between gap-2 px-4 py-2.5"
      >
        <div class="flex items-center gap-2">
          <h2 class="text-[13px] font-semibold">{{ overviewTitle }}</h2>
          <HelpTooltip trigger="click" width-class="w-80">
            <template #trigger>
              <button
                type="button"
                class="rounded-full p-0.5 text-gray-400 hover:text-primary-600"
                :aria-label="t('vendor.cumulativeData')"
              >
                <Icon name="infoCircle" size="sm" />
              </button>
            </template>
            <div class="space-y-1.5 text-xs">
              <p class="font-medium">{{ t('vendor.cumulativeData') }}</p>
              <div class="grid grid-cols-[1fr_auto_auto] gap-x-3 gap-y-1">
                <span>{{ t('vendor.requests') }}</span><strong>{{ metricValue(overview?.all_time_usage?.requests) }}</strong><span>{{ t('vendor.today') }} {{ metricValue(overview?.today_usage?.requests) }}</span>
                <span>{{ t('vendor.successRate') }}</span><strong>{{ metricPercent(overview?.all_time_usage?.success_rate) }}</strong><span>{{ t('vendor.today') }} {{ metricPercent(overview?.today_usage?.success_rate) }}</span>
                <span>{{ t('vendor.totalConsumption') }}</span><strong>{{ formatUsageCost(overview?.all_time_usage?.cost_usd) }}</strong><span>{{ t('vendor.today') }} {{ formatUsageCost(overview?.today_usage?.cost_usd) }}</span>
                <span>{{ t('vendor.fableCost') }}</span><strong>{{ formatUsageCost(overview?.all_time_usage?.fable_cost_usd) }}</strong><span>{{ t('vendor.today') }} {{ formatUsageCost(overview?.today_usage?.fable_cost_usd) }}</span>
                <span>{{ t('vendor.cacheHit') }}</span><strong>{{ displayPercent(overview?.all_time_usage?.cache_hit_rate) }}</strong><span>{{ t('vendor.today') }} {{ displayPercent(overview?.today_usage?.cache_hit_rate) }}</span>
              </div>
            </div>
          </HelpTooltip>
        </div>
        <div class="flex flex-wrap items-center gap-2 text-[11px] text-gray-400">
          <span :title="formatDateTime(overview?.collected_at)">{{
            formatDateTime(overview?.collected_at) || t('vendor.unknown')
          }}</span>
          <span>{{ t('vendor.autoRefresh') }}</span>
          <Select
            v-model="refreshInterval"
            class="w-24"
            :options="[
              { value: '0', label: t('vendor.autoRefreshOff') },
              ...['60', '120', '300'].map((value) => ({
                value,
                label: t('vendor.refreshMinutes', { count: Number(value) / 60 })
              }))
            ]"
            :aria-label="t('vendor.autoRefresh')"
          />
          <button
            class="btn btn-ghost btn-sm"
            :aria-expanded="overviewOpen"
            aria-controls="account-overview"
            @click="overviewOpen = !overviewOpen"
          >
            {{ t(overviewOpen ? 'vendor.collapse' : 'vendor.expand')
            }}<Icon name="chevronDown" size="xs" :class="{ 'rotate-180': overviewOpen }" />
          </button>
        </div>
      </div>
      <div v-show="overviewOpen" id="account-overview">
        <div class="grid gap-4 px-4 pb-3 lg:grid-cols-[minmax(350px,2fr)_3fr]">
          <div
            class="grid grid-cols-[72px_1fr_1fr] items-center gap-x-4 gap-y-1 rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-900/50"
          >
            <div class="row-span-2">
              <p class="text-[11px] text-gray-500">{{ t('vendor.accountCountTotal') }}</p>
              <p class="mt-1 text-2xl font-semibold tabular-nums">{{ display(overview?.total) }}</p>
            </div>
            <div
              v-for="key in ['available', 'cooling', 'abnormal', 'disabled'] as const"
              :key="key"
              class="flex items-center justify-between gap-2"
            >
              <span class="text-[11px] text-gray-500">{{ translated('accountCount_' + key) }}</span
              ><strong
                class="text-base tabular-nums"
                :class="{ 'text-amber-600': key === 'cooling', 'text-red-600': key === 'abnormal' }"
                >{{ display(overview?.[key]) }}</strong
              >
            </div>
          </div>
          <div class="grid grid-cols-2 gap-4 sm:grid-cols-5 lg:gap-3">
            <div>
              <p class="text-[11px] text-gray-500">RPM</p>
              <p class="mt-1 text-lg font-semibold tabular-nums">{{ display(overview?.rpm) }}</p>
              <p class="mt-0.5 text-[11px] leading-4 text-gray-400">
                {{ t('vendor.recentMinute') }}
              </p>
            </div>
            <div>
              <p class="text-[11px] text-gray-500">TPM</p>
              <p class="mt-1 text-lg font-semibold tabular-nums">
                {{ display(overview?.tpm_with_cache) }}
              </p>
              <p class="mt-0.5 text-[11px] text-gray-400">{{ t('vendor.includesCache') }}</p>
            </div>
            <div>
              <p class="text-[11px] text-gray-500">{{ t('vendor.tpmWithoutCache') }}</p>
              <p class="mt-1 text-lg font-semibold tabular-nums">{{ display(overview?.tpm) }}</p>
              <p class="mt-0.5 text-[11px] text-gray-400">{{ t('vendor.inputAndOutput') }}</p>
            </div>
            <div>
              <p class="text-[11px] text-gray-500">{{ t('vendor.liveConcurrency') }}</p>
              <p class="mt-1 text-lg font-semibold tabular-nums">
                {{ display(overview?.current_concurrency)
                }}<span class="text-xs font-normal text-gray-400">
                  / {{ display(overview?.maximum_concurrency) }}</span
                >
              </p>
            </div>
            <div>
              <p class="text-[11px] text-gray-500">{{ t('vendor.quotaLoad') }}</p>
              <p class="mt-1 text-lg font-semibold tabular-nums text-amber-600">
                {{ display(overview?.high_quota) }}
              </p>
              <p class="mt-0.5 text-[11px] text-gray-400">
                ≥ {{ display(overview?.quota_near_limit_percent || 90) }}% ·
                {{ t('vendor.quotaFullAt', { percent: display(overview?.quota_full_percent || 99) }) }}
              </p>
            </div>
          </div>
        </div>
        <div
          v-if="overview?.quota_distribution"
          class="grid gap-5 border-t border-gray-200 px-4 py-3 dark:border-dark-700 md:grid-cols-3"
        >
          <div v-for="(distribution, window) in overview.quota_distribution" :key="window">
            <h3 class="text-xs font-medium">
              {{ t('vendor.' + window) }}
              <span class="font-normal text-gray-400"
                >· {{ Object.values(distribution).reduce((sum, count) => sum + count, 0) }}</span
              >
            </h3>
            <div
              class="my-2 flex h-1 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"
              aria-hidden="true"
            >
              <span
                v-for="key in distributionKeys"
                :key="key"
                :class="distributionColors[key]"
                :style="{ width: distributionPercent(distribution, key) + '%' }"
              />
            </div>
            <div class="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-gray-500">
              <span v-for="key in distributionKeys" :key="key" :title="key === 'unknown' ? t('vendor.quotaUnknownHelp') : undefined"
                >{{ t('vendor.' + key) }} {{ distribution[key] ?? '—' }}</span
              >
            </div>
          </div>
        </div>
        <span class="sr-only">{{ t('vendor.paused') }} {{ display(overview?.paused) }}</span>
      </div>
    </section>
    <div class="card overflow-hidden">
      <form
        class="vendor-filter-bar flex flex-wrap items-center gap-2 p-3"
        @submit.prevent="load(true)"
      >
        <div class="relative w-48 shrink-0">
          <Icon
            name="search"
            size="sm"
            class="pointer-events-none absolute left-2.5 top-2 text-gray-400"
          /><input
            v-model="filters.search"
            class="input !h-8 !rounded-lg !py-1.5 !pl-8 !pr-2 text-xs"
            :aria-label="t('vendor.search')"
            :placeholder="t('vendor.accountSearch')"
          />
        </div>
        <template v-if="filtersOpen">
        <Select
          v-model="filters.platform"
          class="w-32"
          :options="[
            { value: '', label: t('vendor.allPlatforms') },
            ...['anthropic', 'openai'].map((value) => ({
              value,
              label: platformDisplayName(value)
            }))
          ]"
          :aria-label="t('vendor.platform')"
          @update:model-value="() => { if (filters.platform === 'anthropic') filters.authorization_mode = ''; load(true) }"
        />
        <Select
          v-model="filters.status"
          class="w-32"
          :options="[
            { value: '', label: t('vendor.allStatuses') },
            ...['active', 'paused', 'cooling', 'disabled', 'error', 'archived'].map((value) => ({
              value,
              label: t('vendor.' + value)
            }))
          ]"
          :aria-label="t('vendor.status')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="filters.group_id"
          class="w-36"
          :options="[
            { value: '', label: t('vendor.allGroups') },
            ...availableGroups.map((g) => ({ value: String(g.id), label: g.name }))
          ]"
          :aria-label="t('vendor.groups')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="filters.method"
          class="w-36"
          :options="[
            { value: '', label: t('vendor.allMethods') },
            ...['oauth', 'setup-token', 'setup-token-manual', 'refresh_token'].map((value) => ({
              value,
              label: methodLabel(value)
            }))
          ]"
          :aria-label="t('vendor.method')"
          @update:model-value="load(true)"
        />
        <Select
          v-if="filters.platform !== 'anthropic'"
          v-model="filters.authorization_mode"
          class="w-36"
          :options="[{ value: '', label: t('vendor.authorization.allModes') }, ...openAIAuthorizationModes.map(value => ({ value, label: t('vendor.authorization.' + value) }))]"
          :aria-label="t('vendor.authorization.mode')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="sortChoice"
          class="w-36"
          :options="sortOptions"
          :aria-label="t('vendor.sort')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="filters.cooldown"
          class="w-36"
          :options="[
            { value: '', label: t('vendor.accountCooling') },
            { value: 'available', label: t('vendor.accountAvailable') },
            { value: 'account', label: t('vendor.accountCooldown') },
            { value: 'seven_day', label: t('vendor.sevenDayCooling') },
            { value: 'five_hour', label: t('vendor.fiveHourCooling') },
            { value: 'seven_day_fable', label: t('vendor.fableCooling') }
          ]"
          :aria-label="t('vendor.accountCooling')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="filters.schedulability"
          class="w-36"
          :options="[
            { value: '', label: t('vendor.schedulingAvailability') },
            { value: 'schedulable', label: t('vendor.schedulable') },
            { value: 'unschedulable', label: t('vendor.unschedulable') }
          ]"
          :aria-label="t('vendor.schedulingAvailability')"
          @update:model-value="load(true)"
        />
        </template>
        <AccountCreatedRangePicker
          :start="filters.created_from"
          :end="filters.created_to"
          :label="t('vendor.createdAt')"
          @change="applyCreatedRange"
        />
        <button class="btn btn-secondary btn-sm">
          <Icon name="search" size="xs" />{{ t('vendor.apply') }}</button
        ><button type="button" class="btn btn-ghost btn-sm" @click="resetFilters">
          {{ t('vendor.clear') }}
        </button>
        <button type="button" class="btn btn-ghost btn-sm" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen">
          {{ t(filtersOpen ? 'vendor.collapseFilters' : 'vendor.moreFilters') }}
          <Icon name="chevronDown" size="xs" :class="{ 'rotate-180': filtersOpen }" />
        </button>
        <AccountColumnChooser
          :columns="columnOptions"
          :hidden="hiddenColumns.hidden"
          :always-visible="['name', 'status']"
          :label="t('vendor.visibleColumns')"
          @toggle="hiddenColumns.toggle"
          @reset="hiddenColumns.load"
        />
      </form>
      <div
        class="flex flex-wrap items-center gap-2 border-t border-gray-100 px-3 py-2 text-xs dark:border-dark-700"
      >
        <span class="mr-1 text-gray-500">{{ t('vendor.failureCategory') }}</span>
        <button
          type="button"
          class="rounded-md border px-1.5 py-0.5"
          :class="
            !filters.failure_category
              ? 'border-primary-200 bg-primary-50 text-primary-700 dark:bg-primary-900/20'
              : 'border-gray-200 dark:border-dark-600'
          "
          :aria-pressed="!filters.failure_category"
          @click="
            () => {
              filters.failure_category = ''
              load(true)
            }
          "
        >
          {{ t('vendor.all') }}
        </button>
        <button
          v-for="category in failureCategories"
          :key="category"
          type="button"
          class="rounded-md border px-1.5 py-0.5"
          :class="
            filters.failure_category === category
              ? 'border-primary-200 bg-primary-50 text-primary-700 dark:bg-primary-900/20'
              : 'border-gray-200 dark:border-dark-600'
          "
          :aria-pressed="filters.failure_category === category"
          @click="
            () => {
              filters.failure_category = String(category)
              load(true)
            }
          "
        >
          {{ translated('failure_' + category) }}
          {{ overview?.failures ? overview.failures[category] || 0 : '—' }}
        </button>
        <span class="ml-auto text-primary-600" :class="{ 'sr-only': !selected.size }">{{
          t('vendor.selected', { count: selected.size })
        }}</span>
      </div>
      <p v-if="actionError" role="alert" class="px-4 py-2 text-sm text-red-600">
        {{ actionError }}
      </p>
      <p v-if="actionResult" role="status" class="px-4 py-2 text-sm text-primary-600">
        {{ t('vendor.' + actionResult.label) }} ·
        {{
          actionResult.status === 'success'
            ? t('vendor.success')
            : statusLabel(actionResult.status)
        }}<span v-if="actionResult.latency != null"> · {{ display(actionResult.latency) }} ms</span>
      </p>
      <RequestState
        :loading="loading && !items.length"
        :error="error"
        :empty="!items.length"
        @retry="load()"
      >
        <div class="overflow-x-auto">
          <table class="table vendor-accounts-table" :aria-label="t('vendor.accounts')">
            <colgroup>
              <col v-if="!readOnly || scope.admin" :style="{ width: '32px' }" />
              <col :style="{ width: '248px' }" />
              <col v-if="showOwner" :style="{ width: '170px' }" />
              <col v-if="isColumnVisible('status')" :style="{ width: '92px' }" />
              <col v-if="isColumnVisible('groups')" :style="{ width: '100px' }" />
              <col v-if="isColumnVisible('quota')" :style="{ width: '220px' }" />
              <col v-if="isColumnVisible('concurrency')" :style="{ width: '64px' }" />
              <col v-if="isColumnVisible('priority')" :style="{ width: '72px' }" />
              <col v-if="isColumnVisible('today')" :style="{ width: '110px' }" />
              <col v-if="isColumnVisible('total')" :style="{ width: '110px' }" />
              <col v-if="isColumnVisible('notes')" :style="{ width: '120px' }" />
              <col v-if="isColumnVisible('cache')" :style="{ width: '96px' }" />
              <col v-if="isColumnVisible('created')" :style="{ width: '106px' }" />
              <col :style="{ width: '142px' }" />
            </colgroup>
            <thead>
              <tr>
                <th v-if="!readOnly || scope.admin">
                  <input
                    type="checkbox"
                    :aria-label="t('vendor.selectPage')"
                    :checked="allSelected"
                    :disabled="!selectableAccounts.length"
                    @change="
                      selected = selectionForPage(
                        selected,
                        selectableAccounts.map((a) => a.id),
                        ($event.target as HTMLInputElement).checked
                      )
                    "
                  />
                </th>
                <th>{{ t('vendor.nameAndProxy') }}</th>
                <th v-if="showOwner">{{ t('vendor.resourceOwner') }}</th>
                <th v-if="isColumnVisible('status')">{{ t('vendor.status') }}</th>
                <th v-if="isColumnVisible('groups')">{{ t('vendor.groups') }}</th>
                <th v-if="isColumnVisible('quota')">{{ t('vendor.quotaWindows') }}</th>
                <th v-if="isColumnVisible('concurrency')">{{ t('vendor.concurrency') }}</th>
                <th v-if="isColumnVisible('priority')">{{ t('vendor.priority') }}</th>
                <th v-if="isColumnVisible('today')">{{ t('vendor.todayCost') }}</th>
                <th v-if="isColumnVisible('total')">{{ t('vendor.allTimeCost') }}</th>
                <th v-if="isColumnVisible('notes')">{{ t('vendor.notes') }}</th>
                <th v-if="isColumnVisible('cache')">{{ t('vendor.cacheHit') }}</th>
                <th v-if="isColumnVisible('created')">{{ t('vendor.createdAt') }}</th>
                <th class="sticky right-0 z-10">
                  {{ t('vendor.actions') }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in items" :key="a.id">
                <td v-if="!readOnly || scope.admin">
                  <input
                    v-if="!a.archived"
                    type="checkbox"
                    :aria-label="t('vendor.select') + ' ' + a.name"
                    :checked="selected.has(a.id)"
                    @change="
                      selected = selectionForPage(
                        selected,
                        [a.id],
                        ($event.target as HTMLInputElement).checked
                      )
                    "
                  />
                </td>
                <td>
                  <div class="flex items-start gap-2">
                    <span
                      class="mt-0.5 rounded-md p-1.5"
                      :class="platformBadgeLightClass(a.platform)"
                      :title="platformDisplayName(a.platform)"
                      ><PlatformIcon :platform="a.platform" size="xs"
                    /></span>
                    <div class="min-w-0">
                      <button
                        class="block max-w-full truncate text-left font-semibold leading-4 hover:text-primary-600"
                        :title="a.name"
                        @click="showDetail(a, $event)"
                      >
                        {{ a.name }}
                      </button>
                      <p
                        v-if="a.archived"
                        class="mt-1 flex flex-wrap items-center gap-1 text-[11px] text-gray-500"
                      >
                        <span class="badge badge-gray">{{ t('vendor.archived') }}</span>
                        <time :datetime="a.archived_at || undefined" :title="t('vendor.archivedAt')">
                          {{ detailDate(a.archived_at) }}
                        </time>
                      </p>
                      <p class="mt-0.5 flex flex-wrap items-center gap-1 text-[11px] text-gray-400">
                        <span>#{{ a.id }}</span>
                        <span
                          class="rounded px-1.5 py-0.5 font-medium"
                          :class="platformBadgeLightClass(a.platform)"
                        >{{ platformDisplayName(a.platform) }}</span>
                        <span
                          v-for="badge in accountBadges(a)"
                          :key="badge"
                          class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700"
                          >{{ badge }}</span
                        >
                      </p>
                      <p
                        class="mt-0.5 flex max-w-full items-center gap-1 text-[11px] leading-4 text-gray-500"
                        :title="readOnly ? undefined : a.proxy?.endpoint"
                      >
                        <Icon name="globe" size="xs" /><span class="truncate">{{
                          a.proxy?.name || (a.proxy_id ? '#' + a.proxy_id : t('vendor.direct'))
                        }}</span>
                      </p>
                    </div>
                  </div>
                </td>
                <td v-if="showOwner" data-resource-owner>
                  <button v-if="a.vendor_id" class="max-w-full text-left hover:text-primary-600" @click="emit('selectVendor', a.vendor_id)">
                    <span class="block truncate" :title="a.vendor_name">{{ a.vendor_name || t('vendor.unknown') }}</span>
                    <span class="text-xs text-gray-400">#{{ a.vendor_id }}</span>
                  </button>
                  <span v-else>{{ t('vendor.unknown') }}</span>
                </td>
                <td v-if="isColumnVisible('status')">
                  <div class="flex flex-row-reverse items-center justify-end gap-1.5">
                    <AccountStatusIndicator :account="statusAccount(a)" @show-temp-unsched="showDetail(a)" /><Toggle
                      v-if="!readOnly && !a.archived"
                      :model-value="!a.vendor_paused_at"
                      :disabled="busy || a.status === 'disabled'"
                      :aria-label="
                        t(a.vendor_paused_at ? 'vendor.resume' : 'vendor.pause') + ' ' + a.name
                      "
                      class="-mr-3 origin-left scale-75"
                      @update:model-value="action(a, a.vendor_paused_at ? 'resume' : 'pause')"
                    />
                  </div>
                  <p v-if="a.failure_category" class="mt-1 text-xs text-red-600">
                    {{ translated('failure_' + a.failure_category) }}
                  </p>
                  <p
                    v-for="reason in a.blockers"
                    :key="reason"
                    class="max-w-36 text-[10px] leading-3 text-amber-700"
                  >
                    {{ blockerLabel(reason) }}
                  </p>
                </td>
                <td v-if="isColumnVisible('groups')">
                  <div class="flex max-w-36 flex-wrap gap-1">
                    <GroupBadge
                      v-for="g in a.groups"
                      :key="g.id"
                      :name="g.name"
                      :platform="accountGroupPlatform(g, a)"
                      :show-rate="false"
                    />
                  </div>
                </td>
                <td v-if="isColumnVisible('quota')">
                  <div
                    v-for="q in rowQuotas(a)"
                    :key="String(q.name)"
                    class="grid grid-cols-[38px_40px_30px_78px] items-center gap-1 py-0.5 text-[11px]"
                    :title="
                      (typeof q.resets_at === 'string'
                        ? formatDateTime(q.resets_at) + ' · ' + quotaCountdown(q.resets_at)
                        : t('vendor.resetUnknown')) +
                      ' · ' +
                      (a.timezone || overview?.timezone || '')
                    "
                  >
                    <span>{{
                      t(
                        'vendor.' +
                          ({
                            five_hour: 'quotaFiveShort',
                            seven_day: 'quotaSevenShort',
                            seven_day_fable: 'quotaFableShort'
                          }[String(q.name)] || q.name)
                      )
                    }}</span
                    ><span class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-600"
                      ><span
                        class="block h-full rounded-full"
                        :class="
                          Number(q.utilization) >= (overview?.quota_full_percent || 99)
                            ? 'bg-red-500'
                            : Number(q.utilization) >= (overview?.quota_near_limit_percent || 90)
                              ? 'bg-amber-500'
                              : 'bg-primary-500'
                        "
                        :style="{
                          width: Math.min(100, Math.max(0, Number(q.utilization) || 0)) + '%'
                        }" /></span
                    ><span
                      class="text-right tabular-nums"
                      :class="{ 'text-red-600': Number(q.utilization) >= (overview?.quota_full_percent || 99) }"
                      >{{ q.utilization == null ? '—' : display(q.utilization) + '%' }}</span
                    ><time class="text-gray-500">{{ shortReset(q.resets_at) }}</time>
                  </div>
                </td>
                <td v-if="isColumnVisible('concurrency')" class="tabular-nums">
                  {{ display(a.current_concurrency) }}
                  <span class="text-gray-400">/ {{ a.concurrency }}</span>
                </td>
                <td v-if="isColumnVisible('priority')" class="tabular-nums">{{ a.priority }}</td>
                <td v-if="isColumnVisible('today')" class="tabular-nums">{{ formatUsageCost(a.today_usage?.cost_usd) }}</td>
                <td v-if="isColumnVisible('total')" class="tabular-nums">{{ formatUsageCost(a.all_time_usage?.cost_usd) }}</td>
                <td v-if="isColumnVisible('notes')" class="max-w-40 truncate" :title="a.vendor_notes">
                  {{ display(a.vendor_notes) }}
                </td>
                <td v-if="isColumnVisible('cache')" class="tabular-nums" :title="display(a.statistics_at)">
                  {{ displayPercent(a.all_time_usage?.cache_hit_rate) }}
                </td>
                <td v-if="isColumnVisible('created')" class="text-[11px] text-gray-500" :title="formatDateTime(a.created_at)">
                  <p>{{ formatDateTime(a.created_at).split(' ')[0] || '—' }}</p>
                  <p>{{ formatDateTime(a.created_at).split(' ')[1] }}</p>
                </td>
                <td class="sticky right-0 bg-white dark:bg-dark-800">
                  <div class="flex items-center gap-0.5">
                    <button
                      class="account-action"
                      :title="t('vendor.details')"
                      :aria-label="t('vendor.details') + ' ' + a.name"
                      @click="showDetail(a, $event)"
                    >
                      <Icon name="infoCircle" size="sm" /><span class="sr-only">{{
                        t('vendor.details')
                      }}</span></button
                    ><button
                      class="account-action"
                      :title="t('vendor.usage')"
                      :aria-label="t('vendor.usage') + ' ' + a.name"
                      @click="openUsage(a, $event)"
                    >
                      <Icon name="chart" size="sm" /><span class="sr-only">{{
                        t('vendor.usage')
                      }}</span></button
                    ><button
                      v-if="(!readOnly || scope.admin) && !a.archived"
                      class="account-action"
                      :title="t('vendor.edit')"
                      :aria-label="t('vendor.edit') + ' ' + a.name"
                      @click="openForm(a)"
                    >
                      <Icon name="edit" size="sm" /><span class="sr-only">{{
                        t('vendor.edit')
                      }}</span></button
                    ><ActionMenu
                      v-if="!readOnly && !a.archived"
                      compact
                      :label="t('vendor.moreActions') + ' ' + a.name"
                      :disabled="busy"
                      ><button role="menuitem" @click="a.platform === 'openai' ? showDetail(a, $event) : openForm(a, true)">
                        <Icon name="key" size="sm" />{{ t(a.platform === 'openai' ? 'vendor.authorization.management' : 'vendor.reauthorize') }}</button
                      ><button
                        v-for="[label, op] in accountOperations(a)"
                        :key="op"
                        role="menuitem"
                        :disabled="busy"
                        @click="action(a, op)"
                      >
                        <Icon :name="op === 'probe' ? 'play' : 'refresh'" size="sm" />{{
                          t('vendor.' + label)
                        }}</button
                      ><button
                        role="menuitem"
                        :disabled="exporting"
                        @click="openExport(a)"
                      >
                        <Icon name="download" size="sm" />{{ t('vendor.export') }}</button
                      ><button
                        role="menuitem"
                        :disabled="busy || a.status === 'disabled'"
                        @click="action(a, a.vendor_paused_at ? 'resume' : 'pause')"
                      >
                        <Icon :name="a.vendor_paused_at ? 'play' : 'pause'" size="sm" />{{ t(a.vendor_paused_at ? 'vendor.resume' : 'vendor.pause') }}</button
                      ><button role="menuitem" class="text-red-600" @click="openDelete(a)">
                        <Icon name="trash" size="sm" />{{ t('vendor.delete') }}
                      </button></ActionMenu
                    >
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </RequestState>
      <Pagination
        v-if="total"
        :page="page"
        :page-size="pageSize"
        :total="total"
        @update:page="
          ($event) => {
            page = $event
            load()
          }
        "
        @update:page-size="
          ($event) => {
            pageSize = $event
            page = 1
            load()
          }
        "
      />
    </div>
  </section>
  <EditAccountModal v-if="managementAccount" :show="true" :account="managementAccount"
    :proxies="managementProxies" :groups="managementGroups" supplier-scope :allow-authorize="false"
    :save-account="saveManagementAccount" @close="managementAccount = null"
    @updated="managementAccount = null; load()" />
  <BulkEditAccountModal v-if="bulkEditing" :show="true" supplier-scope
    :account-ids="selectedRows.map(row => row.id)"
    :selected-platforms="[...new Set(selectedRows.map(row => row.platform))]"
    :selected-types="[...new Set(selectedRows.map(row => row.type as AccountType))]"
    :proxies="managementProxies" :groups="managementGroups" :allow-authorize="false"
    :load-reference-account="() => loadManagementAccount(selectedRows[0])" :save-batch="saveManagementBatch"
    @close="bulkEditing = false" @updated="load()" />
  <AccountForm
    v-if="!readOnly && vendor && form && !editing?.archived"
    :key="String(editing?.id) + '-' + reauthorize + '-' + authorizationMode"
    :vendor="vendor"
    :scope="scope"
    :account="editing"
    :reauthorize="reauthorize"
    :authorization-mode="authorizationMode"
    @close="closeForm()"
    @saved="closeForm(true)"
  />
  <AccountImport
    v-if="!readOnly && vendor && importing"
    :vendor="vendor"
    :scope="scope"
    @close="importing = false"
    @saved="load()"
  />
  <BaseDialog
    v-if="detail"
    show
    :title="detail.name + ' · ' + t('vendor.details')"
    content-class="account-detail-drawer !fixed inset-y-0 right-0 !h-full !max-h-none !max-w-2xl !rounded-none !border-y-0 !border-r-0"
    body-class="min-h-0 bg-gray-50 dark:bg-dark-900"
    close-on-click-outside
    @close="closeDetail"
    ><div class="space-y-5">
      <p v-if="actionError" role="alert" class="text-red-600">
        {{ actionError }}
      </p>
      <p v-if="actionResult" role="status" class="rounded-lg border p-3 text-sm">
        {{ t('vendor.' + actionResult.label) }} ·
        {{ actionResult.status === 'success' ? t('vendor.success') : actionResult.status }}
        · {{ display(actionResult.latency) }} ms ·
        {{ display(actionResult.collected) }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.accountSnapshot') }}</p>
      <section class="card space-y-3 p-4" data-detail-section="status">

        <div class="flex flex-wrap items-center gap-3">
          <span v-if="detail.archived" class="badge badge-gray">{{ t('vendor.archived') }}</span>
          <div data-detail-field="status"><AccountStatusIndicator :account="statusAccount(detail)" /></div>
          <span class="text-sm" data-detail-field="scheduling">{{
            detail.schedulable === true
              ? t('vendor.schedulable')
              : detail.schedulable === false
                ? t('vendor.notSchedulable')
                : t('vendor.unknown')
          }}</span>
        </div>
        <div class="flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400 dark:text-gray-400" data-detail-section="blockers">
          <p v-for="blocker in blockers" :key="blocker" class="rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-700">
            {{ blockerLabel(blocker) }}
          </p>
          <p v-if="!blockers.length">
            {{ detail.blockers ? t('vendor.noBlockers') : t('vendor.unknown') }}
          </p>
        </div>
      </section>
      <section class="card space-y-3 p-4" data-detail-section="basic">
        <h3 class="font-semibold">{{ t('vendor.basicInfo') }}</h3>
        <dl
          class="grid grid-cols-[110px_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm sm:grid-cols-[150px_minmax(0,1fr)]"
        >
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.account') }} / {{ t('vendor.platform') }}</dt>
          <dd>
            #{{ detail.id }} ·
            <span data-detail-field="platform">{{ platformLabel(detail.platform) }}</span>
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.proxy') }}</dt>
          <dd data-detail-section="proxy" class="break-words">
            <template v-if="detail.proxy_id === null">{{ t('vendor.direct') }}</template>
            <template v-else>
              <div class="flex items-start gap-1">
                <span class="min-w-0 break-all">{{ detailValue(safeProxyEndpoint) }}</span>
                <button
                  v-if="!readOnly && safeProxyEndpoint"
                  type="button"
                  class="account-action shrink-0"
                  :title="t('vendor.copy')"
                  :aria-label="t('vendor.copy') + ' ' + t('vendor.proxy')"
                  data-copy-proxy
                  @click="copyProxyEndpoint"
                >
                  <Icon name="copy" size="sm" />
                </button>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ detailValue(detail.proxy?.name) }} ·
                {{
                  [detail.proxy?.country_code, detail.proxy?.city].filter(Boolean).join(' · ') ||
                  t('vendor.unknown')
                }}
              </p>
              <p class="text-xs text-gray-400">
                {{ translated('regionSources.' + detail.proxy?.region_source) }}
              </p>
            </template>
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.currentAccountOwner') }}</dt>
          <dd><span data-detail-field="vendor">{{ detailValue(detail.vendor_name) }}</span><span v-if="detail.vendor_id" class="text-gray-500 dark:text-gray-400"> · #{{ detail.vendor_id }}</span></dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.method') }}</dt>
          <dd data-detail-field="method">{{ methodLabel(detail.method || detail.type) }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.priority') }}</dt>
          <dd data-detail-field="priority">{{ detailValue(detail.priority) }}</dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.createdAt') }}</dt>
          <dd data-detail-field="createdAt" :title="detail.created_at">
            {{ detailDate(detail.created_at) }}
          </dd>
          <template v-if="detail.archived">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.archivedAt') }}</dt>
            <dd data-detail-field="archivedAt" :title="detail.archived_at || undefined">
              {{ detailDate(detail.archived_at) }}
            </dd>
          </template>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.groups') }}</dt>
          <dd data-detail-section="groups" class="flex flex-wrap gap-1.5">
            <GroupBadge v-for="group in detail.groups" :key="group.id"
              :name="`${group.name} · #${group.id}`" :platform="accountGroupPlatform(group, detail)" :show-rate="false" />
            <span v-if="!detail.groups?.length">{{
              detail.groups ? t('vendor.noGroups') : t('vendor.unknown')
            }}</span>
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.notes') }}</dt>
          <dd class="break-words">{{ detail.vendor_notes || '—' }}</dd>
        </dl>
      </section>
      <AccountAuthorizations v-if="detail.platform === 'openai'" :account="detail" :vendor="vendor" :read-only="readOnly" :busy="busy"
        @authorize="openForm(detail, true, $event)" @update="changeAuthorization" @revoke="revokeTarget = { account: detail, mode: $event }" @refresh="action(detail, 'refresh', $event)" />
      <section class="card space-y-4 p-4" data-detail-section="runtime">
        <h3 class="text-sm font-semibold">{{ t('vendor.runtimeMetrics') }}</h3>
        <dl class="grid grid-cols-3 gap-3">
          <div><dt class="text-xs text-gray-500 dark:text-gray-400">RPM</dt><dd class="mt-1 text-xl font-semibold tabular-nums" data-detail-field="rpm">{{ detailValue(detail.rpm) }}</dd></div>
          <div><dt class="text-xs text-gray-500 dark:text-gray-400">TPM</dt><dd class="mt-1 break-all text-xl font-semibold tabular-nums" data-detail-field="tpm_with_cache">{{ detailValue(detail.tpm_with_cache) }}</dd></div>
          <div><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.liveConcurrency') }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums" data-detail-field="concurrency">{{ detailValue(detail.current_concurrency) }} / {{ detailValue(detail.concurrency) }}</dd></div>
        </dl>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.tokensWithoutCache') }}：<span data-detail-field="tpm">{{ detailValue(detail.tpm) }}</span></p>
      </section>
      <section class="card space-y-4 p-4" data-detail-section="quota">
        <div class="flex w-full flex-wrap items-center justify-between gap-2">
          <h3 class="text-sm font-semibold">{{ t('vendor.quota') }}</h3>
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.quotaSource') }}: {{ translated('quotaSources.' + detail.quota?.source) }}</span>
        </div>
        <p v-if="!quotaRows.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('vendor.notReported') }}</p>
        <div v-for="(q, i) in quotaRows" :key="i" class="space-y-2">
          <div class="flex items-center justify-between gap-3 text-sm">
            <span class="font-medium">{{ t('vendor.' + q.name) }}</span>
            <span class="tabular-nums text-gray-500 dark:text-gray-400">{{ q.utilization == null ? t('vendor.notReported') : display(q.utilization) + '%' }}</span>
          </div>
          <div class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700" aria-hidden="true">
            <div class="h-full rounded-full" :class="Number(q.utilization) >= 99 ? 'bg-red-500' : Number(q.utilization) >= 90 ? 'bg-amber-500' : 'bg-primary-500'" :style="{ width: Math.max(0, Math.min(100, Number(q.utilization) || 0)) + '%' }" />
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            <template v-if="typeof q.resets_at === 'string'">{{ t('vendor.resetAt') }}: {{ formatDateTime(q.resets_at) }}<span v-if="quotaCountdown(q.resets_at)"> · {{ quotaCountdown(q.resets_at) }}</span></template>
            <template v-else>{{ t('vendor.resetUnknown') }}</template>
          </p>
        </div>
        <p v-if="typeof detail.quota?.updated_at === 'string'" class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.collectedAt') }}: {{ formatDateTime(detail.quota.updated_at) }}</p>
      </section>
      <section class="card overflow-hidden" data-detail-section="usage-summary">
        <h3 class="px-4 py-3 text-sm font-semibold">{{ t('vendor.usageSummary') }}</h3>
        <div class="overflow-x-auto">
          <table class="table whitespace-nowrap text-xs">
            <thead><tr><th>{{ t('vendor.period') }}</th><th class="!text-right">{{ t('vendor.requests') }}</th><th class="!text-right">{{ t('vendor.tokens') }}</th><th class="!text-right">{{ t('vendor.cost') }}</th></tr></thead>
            <tbody><tr v-for="[key, label] in [['today_usage', 'today'], ['all_time_usage', 'allHistory']] as const" :key="key" :data-usage-window="key">
              <td>{{ t('vendor.' + label) }}</td>
              <td class="text-right tabular-nums" data-metric="requests">{{ formatUsageNumber(detail[key]?.requests) }}</td>
              <td class="text-right tabular-nums" data-metric="total_tokens">{{ formatUsageNumber(detail[key]?.total_tokens) }}</td>
              <td class="text-right tabular-nums" data-metric="cost_usd">{{ formatUsageCost(detail[key]?.cost_usd) }}</td>
            </tr></tbody>
          </table>
        </div>
        <div v-if="detail.today_usage?.success_rate != null || detail.all_time_usage?.success_rate != null" class="flex flex-wrap gap-x-4 gap-y-2 px-4 pt-3 text-xs text-gray-500 dark:text-gray-400">
          <p v-for="[key, label] in [['today_usage', 'today'], ['all_time_usage', 'allHistory']] as const" :key="key" :data-success-window="key">{{ t('vendor.' + label) }} · {{ t('vendor.successRate') }} {{ displayPercent(detail[key]?.success_rate) }} <span>({{ formatUsageNumber(detail[key]?.successful_requests) }} / {{ formatUsageNumber(detail[key]?.requests) }})</span></p>
        </div>
        <p class="px-4 py-3 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.costNotice') }}</p>
      </section>
      <section class="card space-y-3 p-4" data-detail-section="cooldown">
        <h3 class="text-sm font-semibold">{{ t('vendor.accountCooling') }}</h3>
        <dl v-if="activeCooldowns.length" class="grid grid-cols-[110px_minmax(0,1fr)] gap-x-3 gap-y-3 text-sm">
          <template v-for="[label, value] in activeCooldowns" :key="label">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.' + label) }}</dt>
            <dd :data-detail-field="label" :title="value"><span>{{ detailDate(value) }}</span><span class="ml-2 text-xs text-gray-500 dark:text-gray-400">{{ quotaCountdown(value) }}</span></dd>
          </template>
        </dl>
        <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ cooldownKnown ? t('vendor.noActiveCooldown') : t('vendor.notReported') }}</p>
      </section>
      <section class="card space-y-3 p-4" data-detail-section="limits">
        <h3 class="font-semibold">{{ t('vendor.modelLimits') }}</h3>
        <table v-if="detail.model_limits?.length" class="w-full text-left text-sm">
          <thead>
            <tr>
              <th class="p-2">{{ t('vendor.modelScope') }}</th>
              <th class="p-2">{{ t('vendor.resetAt') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="limit in detail.model_limits" :key="limit.scope" class="border-t">
              <td class="break-all p-2">{{ limit.scope }}</td>
              <td class="p-2" :title="limit.resets_at">
                {{ detailDate(limit.resets_at) }}
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ quotaCountdown(limit.resets_at) }}
                </p>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="text-sm text-gray-500 dark:text-gray-400">
          {{ detail.model_limits ? t('vendor.noModelLimits') : t('vendor.notReported') }}
        </p>
      </section>
      <details class="card p-4">
        <summary class="cursor-pointer text-sm font-medium">{{ t('vendor.moreMetadata') }}</summary>
        <dl
          class="mt-4 grid grid-cols-[110px_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm sm:grid-cols-[150px_minmax(0,1fr)]"
        >
          <template
            v-for="[label, value] in [
              ['verifiedEmail', detail.email],
              ['updatedAt', detailDate(detail.updated_at)],
              ['planType', detail.plan_type],
              [
                'refreshAvailable',
                detail.can_refresh === true
                  ? t('vendor.yes')
                  : detail.can_refresh === false
                    ? t('vendor.no')
                    : t('vendor.unknown')
              ]
            ]"
            :key="String(label)"
          >
            <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.' + label) }}</dt>
            <dd class="break-words" :data-detail-field="label">{{ detailValue(value) }}</dd>
          </template>
          <template
            v-for="[label, value] in [
              ['pausedAt', detail.vendor_paused_at],
              ['expiresAt', detail.expires_at],
              ['subscriptionExpiresAt', detail.subscription_expires_at],
              ['authorizationExpiresAt', detail.authorization_expires_at]
            ]"
            :key="String(label)"
          >
            <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.' + label) }}</dt>
            <dd :data-detail-field="label" :title="value || undefined">{{ detailDate(value) }}</dd>
          </template>
        </dl>
      </details>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.snapshot') }}: {{ detailDate(detail.statistics_at) }} · {{ detail.timezone }}</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-2">
        <button ref="usageDetailOpener" class="btn btn-primary btn-sm" @click="openUsage(detail, $event)"><Icon name="chart" size="sm" />{{ t('vendor.viewUsage') }}</button>
        <div v-if="!readOnly && !detail.archived" class="flex gap-2">
          <button class="btn btn-secondary btn-sm" :disabled="busy || detail.status === 'disabled'" @click="action(detail, detail.vendor_paused_at ? 'resume' : 'pause')"><Icon :name="detail.vendor_paused_at ? 'play' : 'pause'" size="sm" />{{ t(detail.vendor_paused_at ? 'vendor.resume' : 'vendor.pause') }}</button>
          <ActionMenu :label="t('vendor.moreActions')">
            <button v-if="detail.platform !== 'openai'" role="menuitem" :disabled="busy" @click="openForm(detail, true)"><Icon name="key" size="sm" />{{ t('vendor.reauthorize') }}</button>
            <button v-for="[label, op] in accountOperations(detail)" :key="op" role="menuitem" :disabled="busy" @click="action(detail, op)"><Icon :name="op === 'probe' ? 'play' : 'refresh'" size="sm" />{{ t('vendor.' + label) }}</button>
            <button role="menuitem" class="text-red-600" :disabled="busy" @click="openDelete(detail)"><Icon name="trash" size="sm" />{{ t('vendor.delete') }}</button>
          </ActionMenu>
        </div>
      </div>
    </template>
  </BaseDialog>
  <BaseDialog v-if="revokeTarget" show :title="t('vendor.authorization.revoke')" :show-close-button="!busy" :close-on-escape="!busy" @close="revokeTarget = undefined">
    <p class="text-sm">{{ t('vendor.authorization.revokeWarning', { name: revokeTarget.account.name, mode: t('vendor.authorization.' + revokeTarget.mode) }) }}</p>
    <p v-if="actionError" role="alert" class="mt-3 text-sm text-red-600">{{ actionError }}</p>
    <template #footer><div class="flex justify-end gap-2">
      <button class="btn btn-secondary" :disabled="busy" @click="revokeTarget = undefined">{{ t('vendor.cancel') }}</button>
      <button class="btn btn-danger" :disabled="busy" data-testid="confirm-revoke-authorization" @click="changeAuthorization()">{{ t(busy ? 'vendor.loading' : 'vendor.authorization.revoke') }}</button>
    </div></template>
  </BaseDialog>
  <BaseDialog
    v-if="usageId"
    show
    :title="
      (usageAccount?.name || '#' + usageId) +
      ' · ' +
      t('vendor.usageDetails')
    "
    width="extra-wide"
    body-class="bg-gray-50 dark:bg-dark-900"
    @close="closeUsage"
    ><UsageWorkbench
      :admin="scope.admin"
      :read-only="scope.readOnly"
      :vendor-id="usageVendorId"
      :account-id="usageId"
      :groups="availableGroups"
  /></BaseDialog>
  <BaseDialog
    v-if="!readOnly && confirmDelete && !confirmDelete.archived"
    show
    :title="t('vendor.delete')"
    @close="confirmDelete = undefined"
    ><p>{{ t('vendor.deleteAccountWarning') }}</p>
    <p class="my-3 font-semibold">{{ confirmDelete.name }}</p>
    <template #footer>
      <button class="btn btn-secondary" :disabled="busy" @click="confirmDelete = undefined">
        {{ t('vendor.cancel') }}
      </button>
      <button class="btn btn-danger" :disabled="busy" @click="remove">
        {{ t('vendor.confirm') }}
      </button>
    </template></BaseDialog
  >
  <BaseDialog v-if="!readOnly && confirmExport" show :title="t('vendor.export')" @close="confirmExport = false"
    ><p>{{ t('vendor.exportWarning') }}</p>
    <p class="my-3">{{ t('vendor.selected', { count: selected.size }) }}</p>
    <template #footer>
      <button class="btn btn-secondary" :disabled="exporting" @click="confirmExport = false">
        {{ t('vendor.cancel') }}
      </button>
      <button class="btn btn-primary" :disabled="exporting || !selected.size" @click="exportSelected">
        {{ t('vendor.confirm') }}
      </button>
    </template></BaseDialog
  >
  <TotpStepUpDialog v-if="!readOnly" :controller="stepUp" />
</template>

<style scoped>
:global(.modal-enter-from .modal-content.account-detail-drawer),
:global(.modal-leave-to .modal-content.account-detail-drawer) {
  transform: translateX(100%);
}
:global(.modal-enter-to .modal-content.account-detail-drawer),
:global(.modal-leave-from .modal-content.account-detail-drawer) {
  transform: translateX(0);
}
@media (prefers-reduced-motion: reduce) {
  :global(.modal-enter-from .modal-content.account-detail-drawer),
  :global(.modal-leave-to .modal-content.account-detail-drawer) {
    transform: none;
  }
}
.vendor-accounts-table {
  min-width: 1528px;
  table-layout: fixed;
  font-size: 12px;
}
.vendor-accounts-table :is(th, td) {
  @apply whitespace-nowrap px-3 py-2;
}
.vendor-accounts-table th {
  @apply text-xs font-medium text-gray-500;
}
.vendor-accounts-table th.sticky {
  /* Override the shared table header's translucent dark background. */
  @apply bg-gray-50 dark:bg-dark-800;
}
.vendor-accounts-table td {
  @apply border-t border-gray-100 dark:border-dark-700;
}
.vendor-accounts-table :deep(.text-sm) {
  @apply text-xs;
}
.account-action {
  @apply rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700;
}
</style>
