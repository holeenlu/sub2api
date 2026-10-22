<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { vendorAPI, vendorManagementAPI } from '../api'
import { useVendorStore } from '../store'
import {
  dateRange,
  display,
  download,
  errorText,
  statementPeriod,
  toLocalDateTime,
} from '../helpers'
import { useVendorPage } from '../useVendorPage'
import type {
  SettlementPreview,
  Statement,
  Vendor,
  SettlementPayment,
  Query,
} from '../types'
import RequestState from './RequestState.vue'
import UsageTable from './UsageTable.vue'
const props = withDefaults(
  defineProps<{
    admin?: boolean
    payer?: boolean
    vendorId?: number
    vendor?: Vendor
    initialDraft?: Query
  }>(),
  { admin: false, payer: false },
)
const store = useVendorStore()
const canManage = computed(() => props.admin || props.payer)
const payerScope = computed(() => !props.admin && props.payer)
const emit = defineEmits<{ draftConsumed: [] }>()
const { t } = useI18n(),
  filter = reactive({ status: '', start_date: '', end_date: '' }),
  applied = ref({ ...filter })
const statusLabel = (status: string) =>
  t('vendor.' + (status === 'void' ? 'voided' : status))
const statusOptions = computed(() => [
  { value: '', label: t('vendor.all') },
  ...(canManage.value
    ? ['draft', 'pending_payment', 'partially_paid', 'paid', 'void']
    : ['pending_payment', 'partially_paid', 'paid']
  ).map((value) => ({ value, label: statusLabel(value) })),
])
function lateUsageText(value: Statement['late_usage']) {
  if (typeof value === 'number') return `${t('vendor.requests')}: ${value}`
  if (!value) return '—'
  return `${t('vendor.requests')}: ${display(value.requests)} · ${t('vendor.cost')}: ${display(value.cost_usd)}`
}
const list = useVendorPage<Statement>((q, s) =>
  vendorAPI.settlements(
    props.admin,
    {
      ...q,
      ...(canManage.value && props.vendorId ? { vendor_id: props.vendorId } : {}),
    },
    s,
    payerScope.value,
  ),
)
const { items, total, page, pageSize, loading, error } = list
const detail = ref<Statement>(),
  busy = ref(false),
  actionError = ref(''),
  creating = ref(false),
  previews = ref<SettlementPreview[]>([]),
  draftResults = ref<{ vendor_id: number; status: string; error?: string }[]>(
    [],
  ),
  vendors = ref<Vendor[]>([]),
  scopedVendor = ref<Vendor>()
function isEligiblePayee(vendor: Vendor) {
  return props.admin
    ? vendor.tier === 1 && vendor.parent_vendor_id == null
    : payerScope.value && vendor.tier === 2 && vendor.parent_vendor_id === store.vendor?.id
}
const scopeRestriction = computed(() => {
  if (!canManage.value) return ''
  if (payerScope.value && (store.vendor?.tier !== 1 || store.vendor.parent_vendor_id != null))
    return t('vendor.unavailable')
  if (scopedVendor.value && !isEligiblePayee(scopedVendor.value))
    return t(props.admin ? 'vendor.selectParent' : 'vendor.unavailable')
  return ''
})
const canDraft = computed(() => canManage.value && !scopeRestriction.value)
const canManageStatement = computed(() =>
  canDraft.value && detail.value && (props.admin
    ? detail.value.payer_vendor_id == null
    : detail.value.payer_vendor_id === store.vendor?.id),
)
const yesterday = new Date()
yesterday.setDate(yesterday.getDate() - 1)
const draft = reactive({
  vendor_ids: props.vendorId ? [props.vendorId] : ([] as number[]),
  ...dateRange(7, yesterday),
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  exchange_rate: '',
  exchange_rate_source: '',
  default_coefficient: props.vendor?.default_coefficient || '',
  contract_version: props.vendor?.contract_version || undefined,
  adjustment: '0.00',
  adjustment_reason: '',
  account_id: undefined as number | undefined,
  group_id: undefined as number | undefined,
  platform: '',
})
const selectedPayeesValid = computed(() => draft.vendor_ids.length > 0 &&
  draft.vendor_ids.every((id) => vendors.value.some((vendor) => vendor.id === id && isEligiblePayee(vendor))),
)
const pendingPreviews = computed(() => previews.value.filter((p) =>
  p.preview_token && !p.error && p.available_count > 0 &&
  draft.vendor_ids.includes(p.vendor_id) &&
  vendors.value.some((vendor) => vendor.id === p.vendor_id && isEligiblePayee(vendor)) &&
  !draftResults.value.some((r) => r.vendor_id === p.vendor_id && r.status === 'success'),
))
const coefficients = reactive<Record<string, string>>({
  ...props.vendor?.group_coefficients,
})
const contractPeriods = ref<
  {
    start_date: string
    end_date: string
    contract_version: number
    default_coefficient: string
    group_coefficients: Record<string, string>
  }[]
>([])
function addContractPeriod() {
  if (!props.admin || !canDraft.value || !creating.value) return
  contractPeriods.value.push({
    start_date: draft.start_date,
    end_date: draft.end_date,
    contract_version:
      draft.contract_version || props.vendor?.contract_version || 1,
    default_coefficient: draft.default_coefficient,
    group_coefficients: { ...coefficients },
  })
}
const idempotency = new Map<string, string>()
let previewVersion = 0,
  scopeEpoch = 0,
  detailVersion = 0,
  draftVersion = 0
watch(
  [draft, coefficients, contractPeriods],
  () => {
    previewVersion++
    previews.value = []
    draftResults.value = []
  },
  { deep: true, flush: 'sync' },
)
const mutation = ref<
    'confirm' | 'void' | 'payments' | 'reverse' | 'adjustments'
  >(),
  payment = ref<SettlementPayment>(),
  mutationKey = ref('')
const mutationForm = reactive({
  amount: '',
  reason: '',
  paid_at: '',
  channel: '',
  reference: '',
  confirmed: false,
})
const canPay = computed(
  () =>
    detail.value &&
    ['pending_payment', 'partially_paid'].includes(detail.value.status),
)
const canVoid = computed(
  () =>
    detail.value &&
    ['draft', 'pending_payment'].includes(detail.value.status) &&
    Number(detail.value.paid) === 0,
)
const mutationTitle = computed(() =>
  t(
    'vendor.' +
      {
        confirm: 'confirmStatement',
        void: 'void',
        payments: 'registerPayment',
        reverse: 'reversePayment',
        adjustments: 'linkedAdjustment',
      }[mutation.value || 'confirm'],
  ),
)
function load(reset = false) {
  if (reset) applied.value = { ...filter }
  return list.load(applied.value, reset)
}
function resetContract(vendor?: Vendor) {
  draft.default_coefficient = props.admin ? vendor?.default_coefficient || '' : ''
  draft.contract_version = props.admin ? vendor?.contract_version : undefined
  for (const key of Object.keys(coefficients)) delete coefficients[key]
  if (props.admin) Object.assign(coefficients, vendor?.group_coefficients)
  contractPeriods.value = []
}
function closeStatement() {
  detailVersion++
  busy.value = false
  detail.value = undefined
  mutation.value = undefined
  payment.value = undefined
}
function closeDraft() {
  draftVersion++
  previewVersion++
  creating.value = false
  busy.value = false
  previews.value = []
}
watch(
  () => [
    props.vendorId, props.admin, props.payer, props.vendor,
    props.vendor?.tier, props.vendor?.parent_vendor_id,
    ...(payerScope.value ? [store.vendor?.id, store.vendor?.tier, store.vendor?.parent_vendor_id] : []),
  ],
  () => {
    scopeEpoch++
    closeStatement()
    closeDraft()
    busy.value = false
    actionError.value = ''
    total.value = 0
    draftResults.value = []
    vendors.value = []
    scopedVendor.value = props.vendor?.id === props.vendorId ? props.vendor : undefined
    idempotency.clear()
    draft.vendor_ids = props.vendorId ? [props.vendorId] : []
    resetContract(props.vendor?.id === props.vendorId ? props.vendor : undefined)
    if (!canManage.value) filter.status = ''
    load(true)
  },
  { immediate: true, flush: 'sync' },
)
onUnmounted(() => {
  scopeEpoch++
  detailVersion++
  draftVersion++
  previewVersion++
})
async function openStatement(id: number, allocationPage = 1) {
  const epoch = scopeEpoch, version = ++detailVersion
  busy.value = true
  actionError.value = ''
  mutation.value = undefined
  payment.value = undefined
  try {
    const result = await vendorAPI.statement(props.admin, id, {
      page: allocationPage,
      page_size: 20,
    }, payerScope.value)
    if (epoch === scopeEpoch && version === detailVersion) detail.value = result
  } catch (e) {
    if (epoch === scopeEpoch && version === detailVersion) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch && version === detailVersion) busy.value = false
  }
}
async function newDraft(initial?: Query) {
  if (!canDraft.value || busy.value) return
  const epoch = scopeEpoch, version = ++draftVersion, vendorId = props.vendorId
  const management = props.admin ? vendorAPI : vendorManagementAPI(store.vendor!)
  actionError.value = ''
  busy.value = true
  creating.value = false
  previews.value = []
  draftResults.value = []
  draft.vendor_ids = props.vendorId ? [props.vendorId] : []
  vendors.value = []
  resetContract(props.vendor?.id === vendorId ? props.vendor : undefined)
  draft.account_id = initial?.account_id
    ? Number(initial.account_id)
    : undefined
  draft.group_id = initial?.group_id ? Number(initial.group_id) : undefined
  draft.platform = String(initial?.platform || '')
  if (initial) {
    for (const key of ['start_date', 'end_date', 'timezone'] as const)
      if (initial[key]) draft[key] = String(initial[key])
  }
  try {
    if (props.admin && vendorId) {
      // A scoped contract must never be applied to a different supplier.
      const vendor = props.vendor?.id === vendorId ? props.vendor : await management.detail(vendorId)
      if (epoch !== scopeEpoch || version !== draftVersion) return
      if (vendor.id !== vendorId) {
        actionError.value = t('vendor.unavailable')
        return
      }
      scopedVendor.value = vendor
      if (!isEligiblePayee(vendor)) {
        draft.vendor_ids = []
        resetContract()
        return
      }
      vendors.value = [vendor]
      resetContract(vendor)
      creating.value = true
      return
    }
    const all: Vendor[] = []
    let page = 1,
      total = 0
    do {
      const data = await management.list({
        page, page_size: 100, tier: props.admin ? 1 : 2,
        ...(payerScope.value ? { parent_vendor_id: store.vendor!.id } : {}),
      })
      if (epoch !== scopeEpoch || version !== draftVersion) return
      all.push(...data.items)
      total = data.total
      page++
    } while (all.length < total && page <= 100)
    if (vendorId) scopedVendor.value = all.find((vendor) => vendor.id === vendorId)
    vendors.value = all.filter((vendor) => isEligiblePayee(vendor) && (!vendorId || vendor.id === vendorId))
    draft.vendor_ids = draft.vendor_ids.filter((id) => vendors.value.some((vendor) => vendor.id === id))
    if (!vendors.value.length) {
      if (!scopeRestriction.value) actionError.value = t(props.admin ? 'vendor.selectParent' : 'vendor.unavailable')
      return
    }
    creating.value = true
  } catch (e) {
    if (epoch === scopeEpoch && version === draftVersion) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch && version === draftVersion) busy.value = false
  }
}
onMounted(() => {
  if (canManage.value && props.initialDraft) {
    newDraft(props.initialDraft)
    emit('draftConsumed')
  }
})
async function preview() {
  if (!canDraft.value || busy.value || !creating.value || !selectedPayeesValid.value) return
  if (draft.start_date > draft.end_date) {
    actionError.value = t('vendor.invalidDates')
    return
  }
  busy.value = true
  actionError.value = ''
  const epoch = scopeEpoch, version = previewVersion, session = draftVersion
  try {
    const response = await vendorAPI.preview({
      vendor_ids: [...draft.vendor_ids],
      start_date: draft.start_date,
      end_date: draft.end_date,
      timezone: draft.timezone,
      exchange_rate: draft.exchange_rate,
      exchange_rate_source: draft.exchange_rate_source,
      adjustment: draft.adjustment,
      adjustment_reason: draft.adjustment_reason,
      account_id: draft.account_id,
      group_id: draft.group_id,
      platform: draft.platform,
      coefficient_unit: 'CNY/USD',
      settlement_currency: 'USDT',
      ...(props.admin
        ? {
            default_coefficient: draft.default_coefficient || undefined,
            contract_version: draft.contract_version,
            // Omission inherits the supplier contract; {} explicitly clears its overrides.
            group_coefficients: props.vendorId && props.vendor?.id === props.vendorId
              ? Object.fromEntries(
                  Object.entries(coefficients).filter(([, value]) => value.trim()),
                )
              : undefined,
            ...(contractPeriods.value.length
              ? {
                  contract_periods: contractPeriods.value.map((p) => ({
                    ...p,
                    group_coefficients: Object.fromEntries(
                      Object.entries(p.group_coefficients).filter(([, value]) => value.trim()),
                    ),
                  })),
                }
              : {}),
          }
        : {}),
    }, payerScope.value)
    if (epoch !== scopeEpoch || version !== previewVersion) return
    previews.value = Array.isArray(response) ? response : response.items
    for (const p of previews.value)
      if (!idempotency.has(p.preview_token))
        idempotency.set(p.preview_token, crypto.randomUUID())
  } catch (e) {
    if (epoch === scopeEpoch && version === previewVersion) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch && session === draftVersion) busy.value = false
  }
}
async function saveDrafts() {
  if (!canDraft.value || busy.value || !creating.value || !selectedPayeesValid.value || !pendingPreviews.value.length) return
  const epoch = scopeEpoch, version = previewVersion, session = draftVersion
  busy.value = true
  actionError.value = ''
  try {
    const response = await vendorAPI.drafts(
      pendingPreviews.value
        .map((p) => ({
          preview_token: p.preview_token,
          idempotency_key: idempotency.get(p.preview_token)!,
        })),
      payerScope.value,
    )
    if (epoch !== scopeEpoch || version !== previewVersion) return
    draftResults.value = [
      ...draftResults.value.filter((r) => r.status === 'success'),
      ...response.map((r) => ({
        vendor_id: r.vendor_id,
        status: r.error ? 'failed' : 'success',
        error: r.error,
      })),
    ]
    await load()
  } catch (e) {
    if (epoch === scopeEpoch && version === previewVersion) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch && session === draftVersion) busy.value = false
  }
}
function openMutation(
  action: NonNullable<typeof mutation.value>,
  p?: SettlementPayment,
) {
  if (!canManageStatement.value || !detail.value) return
  mutation.value = action
  payment.value = p
  mutationKey.value = crypto.randomUUID()
  Object.assign(mutationForm, {
    amount:
      action === 'payments' ? detail.value?.unpaid || '' : p?.amount || '',
    reason: '',
    paid_at: toLocalDateTime(),
    channel: '',
    reference: '',
    confirmed: false,
  })
}
async function mutate() {
  if (!canManageStatement.value || busy.value || !detail.value || !mutation.value) return
  if (mutation.value === 'payments' && !mutationForm.confirmed) return
  if (mutation.value === 'reverse' && !payment.value) return
  const epoch = scopeEpoch, version = detailVersion
  busy.value = true
  actionError.value = ''
  const action = mutation.value,
    statement = detail.value
  const base = { version: statement.version }
  let body: Record<string, unknown> = base
  if (action === 'void') body = { ...base, reason: mutationForm.reason }
  if (['payments', 'reverse', 'adjustments'].includes(action))
    body = {
      ...base,
      amount: mutationForm.amount,
      idempotency_key: mutationKey.value,
      ...(action === 'payments'
        ? {
            paid_at: new Date(mutationForm.paid_at).toISOString(),
            channel: mutationForm.channel,
            reference: mutationForm.reference,
          }
        : { reason: mutationForm.reason }),
    }
  try {
    const result = await vendorAPI.statementAction(
      statement.id,
      action === 'reverse'
        ? 'payments/' + payment.value!.id + '/reverse'
        : action,
      body,
      payerScope.value,
    )
    if (epoch !== scopeEpoch || version !== detailVersion) return
    mutation.value = undefined
    await openStatement(result?.id || statement.id)
    if (epoch !== scopeEpoch) return
    await load()
  } catch (e) {
    if (epoch === scopeEpoch && version === detailVersion) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch && version === detailVersion) busy.value = false
  }
}
async function exportStatement(id: number) {
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    const data = await vendorAPI.exportStatement(props.admin, id, payerScope.value)
    if (epoch !== scopeEpoch) return
    download(data, 'settlement-' + id + '.csv')
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
</script>
<template>
  <section class="space-y-4">
    <div class="flex flex-wrap justify-between gap-3">
      <h2 class="text-xl font-semibold">
        {{ t(admin ? 'vendor.settlements' : payer ? 'vendor.downstreamPayables' : 'vendor.upstreamReceivables') }}
      </h2>
      <button v-if="canDraft" class="btn btn-primary" :disabled="busy" @click="newDraft()">
        {{ t('vendor.newStatement') }}
      </button>
    </div>
    <p v-if="!canManage" class="text-sm text-gray-500">
      {{ t('vendor.readOnlyBills') }}
    </p>
    <p v-if="scopeRestriction" class="text-sm text-amber-700">
      {{ scopeRestriction }}
    </p>
    <form
      class="card flex flex-wrap items-end gap-3 p-4"
      @submit.prevent="load(true)"
    >
      <div class="min-w-44">
        <p class="mb-1 text-sm">{{ t('vendor.status') }}</p>
        <Select
          v-model="filter.status"
          :options="statusOptions"
          :aria-label="t('vendor.status')"
        />
      </div>
      <label
        >{{ t('vendor.startDate')
        }}<input
          v-model="filter.start_date"
          type="date"
          class="input mt-1" /></label
      ><label
        >{{ t('vendor.endDate')
        }}<input
          v-model="filter.end_date"
          type="date"
          class="input mt-1" /></label
      ><button class="btn btn-primary">{{ t('vendor.apply') }}</button>
    </form>
    <p v-if="actionError" role="alert" class="text-red-600">
      {{ actionError }}
    </p>
    <RequestState
      :loading="loading"
      :error="error"
      :empty="!items.length"
      @retry="load()"
      ><div class="overflow-x-auto rounded-xl border">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th
                v-for="key in [
                  'number',
                  'vendor',
                  'period',
                  'cost',
                  'payable',
                  'paid',
                  'unpaid',
                  'status',
                  'actions',
                ]"
                :key="key"
                class="whitespace-nowrap p-3"
              >
                {{ t('vendor.' + key) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in items" :key="s.id" class="border-t">
              <td class="p-3">
                <button
                  class="font-semibold text-primary-600"
                  @click="openStatement(s.id)"
                >
                  {{ s.number }}
                </button>
              </td>
              <td class="p-3">
                {{ s.payee?.vendor_name || '#' + s.vendor_id }}
              </td>
              <td class="p-3 text-xs">
                {{ statementPeriod(s.start_at, s.end_at, s.timezone) }}
                <p>{{ s.timezone }}</p>
              </td>
              <td
                v-for="key in ['cost_usd', 'total', 'paid', 'unpaid'] as const"
                :key="key"
                class="p-3 tabular-nums"
              >
                {{ s[key] }}
              </td>
              <td class="p-3">{{ statusLabel(s.status) }}</td>
              <td class="p-3">
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="busy"
                  @click="exportStatement(s.id)"
                >
                  {{ t('vendor.export') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div></RequestState
    ><Pagination
      v-if="total"
      :page="page"
      :page-size="pageSize"
      :total="total"
      @update:page="
        page = $event
        load();
      "
      @update:page-size="
        pageSize = $event
        page = 1
        load();
      "
    />
  </section>
  <BaseDialog
    v-if="detail"
    show
    :title="detail.number"
    width="extra-wide"
    @close="closeStatement"
    ><div class="space-y-5">
      <p v-if="actionError" role="alert" class="text-red-600">
        {{ actionError }}
      </p>
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div
          v-for="[key, label] in [
            ['cost_usd', 'cost'],
            ['total', 'payable'],
            ['paid', 'paid'],
            ['credited', 'credited'],
            ['unpaid', 'unpaid'],
          ] as const"
          :key="key"
          class="rounded-xl border p-4"
        >
          <p class="text-xs text-gray-500">{{ t('vendor.' + label) }}</p>
          <p class="text-xl font-semibold">{{ detail[key] }}</p>
        </div>
      </div>
      <dl class="grid grid-cols-2 gap-2 text-sm">
        <template
          v-for="[key, value] in [
            [
              'period',
              statementPeriod(detail.start_at, detail.end_at, detail.timezone),
            ],
            ['timezone', detail.timezone],
            ['snapshot', detail.snapshot_at],
            ['contractVersion', detail.contract_version],
            ['exchangeRate', detail.exchange_rate],
            ['exchangeSource', detail.exchange_rate_source],
            ['adjustment', detail.adjustment],
            ['reason', detail.adjustment_reason],
            ['version', detail.version],
            ['lateUsage', lateUsageText(detail.late_usage)],
            ['parentStatement', detail.parent_statement_id],
          ]"
          :key="String(key)"
          ><dt class="text-gray-500">{{ t('vendor.' + key) }}</dt>
          <dd class="break-words">{{ display(value) }}</dd></template
        >
      </dl>
      <div class="rounded-xl border p-4">
        <h3 class="mb-2 font-semibold">{{ t('vendor.payee') }}</h3>
        <p>
          {{ detail.payee?.vendor_name }} · {{ detail.payee?.contact_name }}
        </p>
        <p>{{ detail.payee?.settlement_email }}</p>
        <p>{{ t('vendor.binanceId') }}: {{ detail.payee?.binance_id }}</p>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr>
              <th
                v-for="key in ['group', 'cost', 'coefficient', 'payable']"
                :key="key"
                class="p-3"
              >
                {{ t('vendor.' + key) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(line, i) in detail.lines" :key="i" class="border-t">
              <td class="p-3">{{ line.group_name || line.group_id }}</td>
              <td class="p-3">{{ line.cost_usd }}</td>
              <td class="p-3">{{ line.coefficient }}</td>
              <td class="p-3">{{ line.amount || line.total }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <section v-if="detail.allocations?.length">
        <h3 class="font-semibold">{{ t('vendor.details') }}</h3>
        <UsageTable
          :rows="detail.allocations"
          :interactive="false"
        /><Pagination
          v-if="detail.allocation_total"
          :page="detail.page || 1"
          :page-size="detail.page_size || 20"
          :total="detail.allocation_total"
          :show-page-size-selector="false"
          @update:page="openStatement(detail.id, $event)"
        />
      </section>
      <div
        v-for="p in detail.payments"
        :key="p.id"
        class="flex flex-wrap items-center gap-3 rounded-lg border p-3"
      >
        <span
          >{{ p.paid_at }} · {{ p.channel }} · {{ p.reference }} ·
          {{ p.amount }}</span
        ><button
          v-if="canManageStatement && Number(p.amount) > 0"
          class="btn btn-secondary btn-sm"
          @click="openMutation('reverse', p)"
        >
          {{ t('vendor.reversePayment') }}
        </button>
      </div>
      <Pagination
        v-if="detail.payment_total && detail.payment_total > 20"
        :page="detail.page || 1"
        :page-size="20"
        :total="detail.payment_total"
        :show-page-size-selector="false"
        @update:page="openStatement(detail.id, $event)"
      />
      <div class="flex flex-wrap gap-2">
        <button
          v-if="canManageStatement && detail.status === 'draft'"
          class="btn btn-primary"
          @click="openMutation('confirm')"
          :disabled="busy || Date.parse(detail.end_at) > Date.now()"
        >
          {{ t('vendor.confirmStatement') }}</button
        ><button
          v-if="canManageStatement && canPay"
          class="btn btn-primary"
          @click="openMutation('payments')"
        >
          {{ t('vendor.registerPayment') }}</button
        ><button
          v-if="canManageStatement && canVoid"
          class="btn btn-secondary"
          @click="openMutation('void')"
        >
          {{ t('vendor.void') }}</button
        ><button
          v-if="
            canManageStatement &&
            ['pending_payment', 'partially_paid', 'paid'].includes(
              detail.status,
            )
          "
          class="btn btn-secondary"
          @click="openMutation('adjustments')"
        >
          {{ t('vendor.linkedAdjustment') }}</button
        ><button class="btn btn-secondary" @click="exportStatement(detail.id)">
          {{ t('vendor.export') }}
        </button>
      </div>
      <p
        v-if="
          canManageStatement &&
          detail.status === 'draft' &&
          Date.parse(detail.end_at) > Date.now()
        "
        class="text-sm text-amber-700"
      >
        {{ t('vendor.closedPeriod') }}
      </p>
    </div></BaseDialog
  >
  <BaseDialog
    v-if="canDraft && creating"
    show
    :title="t('vendor.newStatement')"
    width="extra-wide"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="closeDraft"
    ><div class="space-y-4">
      <p v-if="actionError" role="alert" class="text-red-600">
        {{ actionError }}
      </p>
      <p
        v-if="draft.account_id || draft.group_id || draft.platform"
        class="rounded-lg border p-3 text-sm"
      >
        {{ t('vendor.account') }}: {{ display(draft.account_id) }} ·
        {{ t('vendor.group') }}: {{ display(draft.group_id) }} ·
        {{ t('vendor.platform') }}:
        {{ draft.platform ? platformDisplayName(draft.platform) : '—' }}
      </p>
      <form
        class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3"
        @submit.prevent="preview"
      >
        <fieldset class="col-span-full" :disabled="busy">
          <legend>{{ t('vendor.vendor') }}</legend>
          <div class="max-h-36 overflow-auto">
            <label
              v-for="v in vendors"
              :key="v.id"
              class="mr-4 inline-flex items-center gap-2"
              ><input
                v-model="draft.vendor_ids"
                type="checkbox"
                :value="v.id"
              />{{ v.name }}</label
            >
          </div>
        </fieldset>
        <label
          >{{ t('vendor.startDate')
          }}<input
            v-model="draft.start_date"
            type="date"
            required
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.endDate')
          }}<input
            v-model="draft.end_date"
            type="date"
            required
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.timezone')
          }}<input
            v-model="draft.timezone"
            required
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.exchangeRate')
          }}<input
            v-model="draft.exchange_rate"
            inputmode="decimal"
            pattern="[0-9]+([.][0-9]{1,10})?"
            required
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.exchangeSource')
          }}<input
            v-model="draft.exchange_rate_source"
            required
            class="input mt-1" /></label
        ><label v-if="admin"
          >{{ t('vendor.coefficient')
          }}<input
            v-model="draft.default_coefficient"
            inputmode="decimal"
            pattern="[0-9]+([.][0-9]{1,10})?"
            class="input mt-1" /></label
        ><label v-if="admin"
          >{{ t('vendor.contractVersion')
          }}<input
            v-model.number="draft.contract_version"
            type="number"
            min="1"
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.adjustment')
          }}<input
            v-model="draft.adjustment"
            inputmode="decimal"
            pattern="-?[0-9]+(\.[0-9]{1,2})?"
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.reason')
          }}<input
            v-model="draft.adjustment_reason"
            :required="Number(draft.adjustment) !== 0"
            class="input mt-1" /></label
        ><label v-for="g in admin && vendor?.id === vendorId ? vendor?.groups : []" :key="g.id"
          >{{ g.name }} · {{ t('vendor.coefficient')
          }}<input
            v-model="coefficients[String(g.id)]"
            inputmode="decimal"
            class="input mt-1"
        /></label>
        <fieldset
          v-if="admin"
          class="col-span-full space-y-3 rounded-xl border p-4"
          :disabled="busy"
        >
          <legend class="font-semibold">
            {{ t('vendor.contractPeriods') }}
          </legend>
          <p class="text-sm text-gray-500">
            {{ t('vendor.contractPeriodsHelp') }}
          </p>
          <div
            v-for="(period, index) in contractPeriods"
            :key="index"
            class="grid gap-3 rounded-lg border p-3 sm:grid-cols-2 lg:grid-cols-4"
          >
            <label
              >{{ t('vendor.startDate')
              }}<input
                v-model="period.start_date"
                type="date"
                required
                class="input mt-1"
            /></label>
            <label
              >{{ t('vendor.endDate')
              }}<input
                v-model="period.end_date"
                type="date"
                required
                class="input mt-1"
            /></label>
            <label
              >{{ t('vendor.contractVersion')
              }}<input
                v-model.number="period.contract_version"
                type="number"
                min="1"
                required
                class="input mt-1"
            /></label>
            <label
              >{{ t('vendor.coefficient')
              }}<input
                v-model="period.default_coefficient"
                inputmode="decimal"
                pattern="[0-9]+([.][0-9]{1,6})?"
                required
                class="input mt-1"
            /></label>
            <label v-for="g in vendor?.id === vendorId ? vendor?.groups : []" :key="g.id"
              >{{ g.name }} · {{ t('vendor.coefficient')
              }}<input
                v-model="period.group_coefficients[String(g.id)]"
                inputmode="decimal"
                pattern="[0-9]+([.][0-9]{1,6})?"
                class="input mt-1"
            /></label>
            <button
              type="button"
              class="btn btn-secondary self-end"
              @click="contractPeriods.splice(index, 1)"
            >
              {{ t('vendor.delete') }}
            </button>
          </div>
          <button
            type="button"
            class="btn btn-secondary"
            @click="addContractPeriod"
          >
            {{ t('vendor.addContractPeriod') }}
          </button>
        </fieldset>
        <button
          class="btn btn-primary self-end"
          :disabled="busy || !selectedPayeesValid"
        >
          {{ t('vendor.preview') }}
        </button>
      </form>
      <div
        v-for="p in previews"
        :key="p.vendor_id"
        class="rounded-xl border p-4"
      >
        <p v-if="p.error" role="alert" class="text-red-600">{{ p.error }}</p>
        <h3 class="font-semibold">
          {{ p.statement?.payee?.vendor_name || '#' + p.vendor_id }}
        </h3>
        <p>
          {{ t('vendor.cost') }}: {{ p.statement?.cost_usd }} ·
          {{ t('vendor.payable') }}: {{ p.statement?.total }}
        </p>
        <p>
          {{ t('vendor.availableCount') }}: {{ p.available_count }} ·
          {{ t('vendor.occupiedCount') }}: {{ p.occupied_count }}
        </p>
        <p v-if="p.statement">
          {{ t('vendor.contractVersion') }}: {{ p.statement.contract_version }}
        </p>
        <p class="text-xs text-gray-500">
          {{ t('vendor.expiresAt') }}: {{ p.expires_at }}
        </p>
        <p v-for="(line, i) in p.statement?.lines" :key="i">
          {{ line.group_name }} · {{ line.cost_usd }} × {{ line.coefficient }} →
          {{ line.amount || line.total }}
        </p>
      </div>
      <button
        v-if="pendingPreviews.length"
        class="btn btn-primary"
        :disabled="
          busy || !selectedPayeesValid ||
          pendingPreviews.some((p) => Date.parse(p.expires_at) <= Date.now())
        "
        @click="saveDrafts"
      >
        {{ t('vendor.save') }}
      </button>
      <p v-for="r in draftResults" :key="r.vendor_id">
        #{{ r.vendor_id }} · {{ t('vendor.' + r.status) }} · {{ r.error }}
      </p>
    </div></BaseDialog
  >
  <BaseDialog
    v-if="canManageStatement && mutation && detail"
    show
    :title="mutationTitle"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="mutation = undefined"
    ><form class="space-y-4" @submit.prevent="mutate">
      <p v-if="actionError" role="alert" class="text-red-600">
        {{ actionError }}
      </p>
      <p>
        {{ detail.number }} · {{ t('vendor.version') }} {{ detail.version }}
      </p>
      <label
        v-if="['payments', 'reverse', 'adjustments'].includes(mutation)"
        class="block"
        >{{ t('vendor.amount') }} (USDT)<input
          v-model="mutationForm.amount"
          required
          inputmode="decimal"
          :pattern="
            mutation === 'adjustments'
              ? '-?[0-9]+([.][0-9]{1,2})?'
              : '[0-9]+([.][0-9]{1,2})?'
          "
          class="input mt-1" /></label
      ><template v-if="mutation === 'payments'"
        ><p class="text-sm text-amber-700">{{ t('vendor.noTransfer') }}</p>
        <label class="block"
          >{{ t('vendor.paymentDate')
          }}<input
            v-model="mutationForm.paid_at"
            type="datetime-local"
            required
            class="input mt-1" /></label
        ><label class="block"
          >{{ t('vendor.channel')
          }}<input
            v-model="mutationForm.channel"
            required
            class="input mt-1" /></label
        ><label class="block"
          >{{ t('vendor.reference')
          }}<input
            v-model="mutationForm.reference"
            required
            class="input mt-1" /></label
        ><label class="flex items-center gap-2"
          ><input v-model="mutationForm.confirmed" type="checkbox" required />{{
            t('vendor.paymentConfirmed')
          }}</label
        ></template
      ><label
        v-if="['void', 'reverse', 'adjustments'].includes(mutation)"
        class="block"
        >{{ t('vendor.reason')
        }}<textarea
          v-model="mutationForm.reason"
          required
          class="input mt-1"
        /></label
      ><button
        class="btn btn-primary"
        :disabled="
          busy ||
          (['payments', 'reverse'].includes(mutation) &&
            Number(mutationForm.amount) <= 0)
        "
      >
        {{ t('vendor.confirm') }}
      </button>
    </form></BaseDialog
  >
</template>
