<script setup lang="ts">
import { computed as supplyComputed, watch as watchSupplyScope } from 'vue'
import { useSupplyRequest } from '@/features/supply/useSupplyRequest'
const supplierRequestScope = supplyComputed(() => services.scopeKey.value === null ? null : JSON.stringify([services.scopeKey.value, props.vendor?.id, props.vendor?.policy_version, props.vendor?.contract_version, props.parent?.id, props.parent?.policy_version, props.parent?.contract_version]))

import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import { useSupplyServices } from '../services'
const services = useSupplyServices()
const platformDisplayName = services.platformName
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { generateRandomPassword } from '@/shared/lib/password'
import { vendorAPI, vendorManagementAPI } from '../api'
import { errorText, formatContractDecimal, openAIAuthorizationModes, vendorOpenAIModes } from '../helpers'
import type { OpenAIAuthorizationMode, Vendor, VendorGroup, VendorPlatform } from '../types'
const props = defineProps<{ vendor?: Vendor; parent?: Vendor }>()
const supplyRequests = useSupplyRequest(supplierRequestScope)

const emit = defineEmits<{ close: []; saved: [vendor: Vendor] }>()
const { t } = useI18n()
const busy = ref(false), error = ref(''), groups = ref<VendorGroup[]>([])
const email = ref(''), password = ref('')
const parents = ref<Vendor[]>([]), parentSearch = ref(''), parentsBusy = ref(false)
const selectedParent = ref<Vendor | undefined>(props.parent)
const api = computed(() => vendorManagementAPI(props.parent))
const initialForm = (clear = false) => {
  const vendor = clear ? undefined : props.vendor, parent = clear ? undefined : props.parent
  return ({
  name: vendor?.name || '',
  tier: vendor?.tier ?? (parent ? 2 : 1),
  parent_vendor_id: vendor?.parent_vendor_id ?? parent?.id ?? null,
  status: vendor?.status || 'active',
  policy_version: vendor?.policy_version || 1,
  platforms: [...(vendor?.platforms || parent?.platforms || ['anthropic', 'openai'])] as VendorPlatform[],
  openai_authorization_modes: [...(vendor ? vendorOpenAIModes(vendor) : parent ? vendorOpenAIModes(parent) : ['native'])] as OpenAIAuthorizationMode[],
  group_ids: [...(vendor?.group_ids || [])],
  account_limit: vendor?.account_limit ?? 0,
  proxy_limit: vendor?.proxy_limit ?? 0,
  allow_direct: vendor?.allow_direct ?? false,
  contact_name: vendor?.contact_name || '',
  settlement_email: vendor?.settlement_email || '',
  binance_id: vendor?.binance_id || '',
  default_exchange_rate: formatContractDecimal(vendor?.default_exchange_rate),
  default_coefficient: formatContractDecimal(vendor?.default_coefficient || '1'),
  group_coefficients: Object.fromEntries(Object.entries(vendor?.group_coefficients || {}).map(([id, price]) => [id, formatContractDecimal(price)])),
})
}
const form = reactive(initialForm())
const actorValid = ref(true)
const actorAtMount = services.scopeKey.value
const availableGroups = computed(() => form.tier === 2 ? (selectedParent.value?.groups || []) : groups.value)
const availablePlatforms = computed(() => form.tier === 2 ? (selectedParent.value?.platforms || []) : ['anthropic', 'openai'])
const availableOpenAIModes = computed(() => form.tier === 2 ? vendorOpenAIModes(selectedParent.value) : [...openAIAuthorizationModes])
const validOpenAIModes = computed(() => !form.platforms.includes('openai') ||
  (form.openai_authorization_modes.length > 0 && form.openai_authorization_modes.every(mode => availableOpenAIModes.value.includes(mode))))
watch(availableOpenAIModes, modes => {
  if (form.tier === 2 && !selectedParent.value) return
  form.openai_authorization_modes = form.openai_authorization_modes.filter(mode => modes.includes(mode))
}, { immediate: true })
watch(() => props.parent, parent => { selectedParent.value = parent })
const parentOptions = computed(() => [
  { value: null, label: t('vendor.selectParent'), disabled: true },
  ...(selectedParent.value && !parents.value.some(p => p.id === selectedParent.value?.id) ? [{ value: selectedParent.value.id, label: selectedParent.value.name }] : []),
  ...parents.value.map(p => ({ value: p.id, label: p.name + ' #' + p.id }))
])
let parentEpoch = 0
async function searchParents() {
  const request = supplyRequests.begin('searchParents')
  if (!request.isCurrent()) return

  parentsBusy.value = true
  try {
    const result = await request.wait(vendorAPI.list({ tier: 1, status: 'active', search: parentSearch.value, page_size: 100 }, request.signal))
    parents.value = result.items.filter(p => p.tier === 1 && p.status === 'active')
  } catch (e) {
    if (!request.isCurrent()) return
    error.value = errorText(e)
  }
  finally {
    if (request.isCurrent()) {
      parentsBusy.value = false
    }
  }

}
watch(() => form.parent_vendor_id, async id => {
  const request = supplyRequests.begin('watch1')
  if (!request.isCurrent()) return
  try {

    if (props.parent || props.vendor) return
    const epoch = ++parentEpoch
    selectedParent.value = undefined
    form.group_ids = []
    form.allow_direct = false
    if (!id || form.tier !== 2) return
    try {
      const parent = await request.wait(vendorAPI.detail(id, { signal: request.signal }))
      if (epoch !== parentEpoch) return
      selectedParent.value = parent
      form.platforms = [...parent.platforms]
      form.openai_authorization_modes = [...vendorOpenAIModes(parent)]
    } catch (e) {
      if (!request.isCurrent()) return
      if (epoch === parentEpoch) error.value = errorText(e)
    }

  } catch (failure) {
    if (!request.isCurrent()) return
    throw failure
  }
})
watch(() => form.tier, tier => {
  if (props.vendor || props.parent) return
  form.parent_vendor_id = null
  form.group_ids = []
  form.openai_authorization_modes = tier === 1 ? ['native'] : []
  if (tier === 2) void searchParents()
})
async function save() {
  if (!actorValid.value || busy.value || !validOpenAIModes.value || (form.tier === 2 && !selectedParent.value)) return

  const request = supplyRequests.begin('save')
  if (!request.isCurrent()) return


  busy.value = true
  error.value = ''
  try {
    const group_coefficients = Object.fromEntries(Object.entries(form.group_coefficients).filter(([id, value]) => form.group_ids.includes(Number(id)) && value.trim()))
    const { policy_version, ...policy } = form
    const body = {
      ...policy, openai_authorization_modes: form.platforms.includes('openai') ? [...form.openai_authorization_modes] : [], ...(props.vendor ? { policy_version } : {}), parent_vendor_id: form.tier === 2 ? form.parent_vendor_id : null, group_coefficients,
      ...(!props.vendor ? { email: email.value.trim(), password: password.value } : {})
    }
    const result = props.vendor ? await request.wait(api.value.update(props.vendor.id, body, { signal: request.signal })) : await request.wait(api.value.create(body, { signal: request.signal }))
    password.value = ''
    emit('saved', result)
  } catch (e) {
    if (!request.isCurrent()) return
    error.value = errorText(e)
  }
  finally {
    if (request.isCurrent()) {
      busy.value = false
    }
  }

}
async function loadOptions() {
  if (!actorValid.value) return
  const request = supplyRequests.begin('onMounted0')
  if (!request.isCurrent()) return
  try {

    try {
      if (!props.parent) {
        groups.value = await request.wait(services.getAssignableGroups({ signal: request.signal }))
        if (props.vendor?.tier === 2 && props.vendor.parent_vendor_id) selectedParent.value = await request.wait(vendorAPI.detail(props.vendor.parent_vendor_id, { signal: request.signal }))
      }
    } catch (e) {
      if (!request.isCurrent()) return
      error.value = errorText(e)
    }

  } catch (failure) {
    if (!request.isCurrent()) return
    throw failure
  }
}
onMounted(loadOptions)
onUnmounted(() => { password.value = ''; parentEpoch++ })

watchSupplyScope(services.scopeKey, () => {
  actorValid.value = false
  emit('close')
}, { flush: 'sync' })
watchSupplyScope(supplierRequestScope, () => {
  busy.value = false
  error.value = ''
  groups.value = []
  email.value = ''
  password.value = ''
  parents.value = []
  parentSearch.value = ''
  parentsBusy.value = false
  parentEpoch++
  const sameActor = actorAtMount === services.scopeKey.value
  selectedParent.value = sameActor ? props.parent : undefined
  Object.assign(form, initialForm(!sameActor))
  if (sameActor && actorValid.value) void loadOptions()
}, { flush: 'sync' })
</script>
<template>
  <BaseDialog
    show
    :title="t(vendor ? 'vendor.editSupplier' : 'vendor.createSupplier')"
    width="extra-wide"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="emit('close')"
    ><form class="space-y-5" @submit.prevent="save">
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <fieldset v-if="!vendor" :disabled="busy" class="grid gap-4 rounded-2xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/40 sm:grid-cols-2">
        <label>{{ t('vendor.email') }}<input data-testid="vendor-new-email" v-model="email" required type="email" maxlength="254" autocomplete="off" class="input mt-1" /></label>
        <div>
          <label for="vendor-new-password" class="block">{{ t('vendor.loginPassword') }}</label>
          <div class="mt-1 flex gap-2">
            <input id="vendor-new-password" data-testid="vendor-new-password" v-model="password" required type="text" minlength="6" maxlength="72" autocomplete="new-password" :placeholder="t('admin.users.enterPassword')" class="input min-w-0 flex-1" />
            <button type="button" class="btn btn-secondary shrink-0 px-3" :title="t('admin.users.generatePassword')" :aria-label="t('admin.users.generatePassword')" @click="password = generateRandomPassword()"><Icon name="refresh" size="md" /></button>
          </div>
          <p class="input-hint">{{ t('auth.passwordHint') }}</p>
        </div>
        <label class="sm:col-span-2">{{ t('vendor.supplierTier') }}<Select v-model="form.tier" :disabled="!!parent || busy" :options="[{value:1,label:t('vendor.tier1')},{value:2,label:t('vendor.tier2')}]" :aria-label="t('vendor.supplierTier')" class="mt-1" /></label>
        <p class="sm:col-span-2 text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.createCredentialsHelp') }}</p>
        <div v-if="form.tier === 2" class="sm:col-span-2 space-y-2">
          <label class="block">{{ t('vendor.parentVendor') }}<Select v-model="form.parent_vendor_id" :disabled="!!parent || busy || parentsBusy" :options="parentOptions" :aria-label="t('vendor.parentVendor')" class="mt-1" /></label>
          <div v-if="!parent" class="flex gap-2"><input v-model="parentSearch" :aria-label="t('vendor.searchParent')" :placeholder="t('vendor.searchParent')" class="input" @keydown.enter.prevent="searchParents" /><button type="button" class="btn btn-secondary shrink-0" :disabled="parentsBusy" @click="searchParents">{{ t('vendor.search') }}</button></div>
        </div>
      </fieldset>
      <p v-else class="text-sm text-gray-500">{{ vendor.email }} · {{ t(vendor.tier === 2 ? 'vendor.tier2' : 'vendor.tier1') }} · {{ t('vendor.policyVersion') }} {{ vendor.policy_version }}</p>
      <fieldset
        :disabled="busy"
        class="grid gap-4 rounded-2xl border border-gray-200 p-4 dark:border-dark-700 sm:grid-cols-2 lg:grid-cols-3"
      >
        <label class="sm:col-span-2 lg:col-span-2"
          >{{ t('vendor.name')
          }}<input data-testid="vendor-name" v-model="form.name" required class="input mt-1" /></label
        ><label
          >{{ t('vendor.status')
          }}<Select
            v-model="form.status"
            :options="
              ['active', 'disabled'].map((value) => ({
                value,
                label: t('vendor.' + value),
              }))
            "
            :aria-label="t('vendor.status')"
            class="mt-1 min-w-0"
            :disabled="busy"
        /></label>
        <p
          v-if="vendor && form.status !== 'active'"
          class="text-sm text-amber-700"
        >
          {{ t('vendor.disableWarning') }} {{ t('vendor.accounts') }}:
          {{ vendor.account_tree_count ?? vendor.account_count }}
        </p>
        <fieldset class="col-span-full rounded-xl bg-gray-50 p-3 dark:bg-dark-900/50">
          <legend class="font-medium">{{ t('vendor.platformAndGroups') }}</legend>
          <div class="mt-2 flex flex-wrap gap-x-5 gap-y-2">
            <label
              v-for="p in availablePlatforms"
              :key="p"
              class="inline-flex items-center gap-2"
              ><input v-model="form.platforms" type="checkbox" :value="p" />{{
                platformDisplayName(p)
              }}</label
            >
          </div>
          <fieldset v-if="form.platforms.includes('openai')" class="mt-3 space-y-2 border-l-2 border-primary-200 pl-4 dark:border-primary-800">
            <legend class="text-sm font-medium">{{ t('vendor.authorization.openaiModes') }}</legend>
            <div class="flex flex-wrap gap-4">
              <label v-for="mode in openAIAuthorizationModes" :key="mode" class="inline-flex items-center gap-2 text-sm" :class="{ 'text-gray-400': !availableOpenAIModes.includes(mode) }">
                <input v-model="form.openai_authorization_modes" type="checkbox" :value="mode" :data-testid="'vendor-mode-' + mode" :disabled="!availableOpenAIModes.includes(mode)" />
                {{ t('vendor.authorization.' + mode) }}
                <span v-if="!availableOpenAIModes.includes(mode)" class="text-xs">{{ t('vendor.authorization.parentUnavailable') }}</span>
              </label>
            </div>
            <p class="text-xs text-gray-500">{{ t('vendor.authorization.policyHelp') }}</p>
            <p v-if="!validOpenAIModes" role="alert" class="text-xs text-amber-700 dark:text-amber-400">{{ t('vendor.authorization.requiredMode') }}</p>
          </fieldset>
          <p class="mt-2 text-xs text-gray-500">{{ t('vendor.platformAndGroupsHelp') }}</p>
        </fieldset>
        <fieldset class="col-span-full rounded-xl bg-gray-50 p-3 dark:bg-dark-900/50">
          <legend>{{ t('vendor.groups') }}</legend>
          <div class="mt-2 max-h-40 overflow-y-auto">
            <label
              v-for="g in availableGroups.filter(
                (g) =>
                  !g.platform || g.platform === 'composite' || form.platforms.some((p) => p === g.platform),
              )"
              :key="g.id"
              class="mr-4 inline-flex items-center gap-2"
              ><input
                v-model="form.group_ids"
                type="checkbox"
                :value="g.id"
              />{{ g.name }}</label
            >
          </div>
        </fieldset>
        <div class="col-span-full border-t border-gray-100 pt-2 text-sm font-semibold dark:border-dark-700">{{ t('vendor.resourceDefaults') }}</div>
        <label
          v-for="[key, label] in [
            ['account_limit', 'accountLimit'],
            ['proxy_limit', 'proxyLimit'],
          ] as const"
          :key="key"
          >{{ t('vendor.' + label)
          }}<input
            v-model.number="form[key]"
            type="number"
            min="0" max="10000" step="1"
            :data-testid="key"
            required
            class="input mt-1" /><span v-if="key === 'account_limit' || key === 'proxy_limit'" class="input-hint">{{ t('vendor.unlimitedResourceHelp') }}</span></label
        ><label class="flex items-center gap-2"
          ><input v-model="form.allow_direct" type="checkbox" :disabled="form.tier === 2 && !selectedParent?.allow_direct" />{{
            t('vendor.allowDirect')
          }}</label
        ><div class="col-span-full border-t border-gray-100 pt-2 text-sm font-semibold dark:border-dark-700">{{ t('vendor.settlementProfile') }}</div><label
          >{{ t('vendor.contact')
          }}<input v-model="form.contact_name" class="input mt-1" /></label
        ><label
          >{{ t('vendor.settlementEmail')
          }}<input
            v-model="form.settlement_email"
            type="email"
            class="input mt-1" /></label
        ><label
          >{{ t('vendor.binanceId')
          }}<input
            v-model="form.binance_id"
            type="text"
            inputmode="numeric"
            class="input mt-1" /></label
        >
        <div class="col-span-full grid grid-cols-1 items-start gap-3 md:grid-cols-3">
          <label
            >{{ t('vendor.defaultSettlementPrice')
            }}<input
              v-model="form.default_coefficient"
              data-testid="vendor-default-price"
              @blur="form.default_coefficient = formatContractDecimal(form.default_coefficient)"
              inputmode="decimal"
              required
              pattern="[0-9]+([.][0-9]{1,8})?"
              :title="t('vendor.contractPricePrecision')"
              class="input mt-1" />
            <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.defaultSettlementPriceHelp') }}</span></label
          ><label
            v-for="g in availableGroups.filter((g) => form.group_ids.includes(g.id))"
            :key="g.id"
            >{{ t('vendor.groupSettlementPrice', { group: g.name })
            }}<input
              v-model="form.group_coefficients[String(g.id)]"
              :data-testid="'vendor-group-price-' + g.id"
              @blur="form.group_coefficients[String(g.id)] = formatContractDecimal(form.group_coefficients[String(g.id)])"
              inputmode="decimal"
              pattern="[0-9]+([.][0-9]{1,8})?"
              :placeholder="t('vendor.inheritDefaultPrice', { price: form.default_coefficient })"
              :title="t('vendor.contractPricePrecision')"
              class="input mt-1"
          /></label>
        </div>
        <label>
          {{ t('vendor.settlementExchangeRate') }}
          <input v-model="form.default_exchange_rate" data-testid="vendor-exchange-rate"
            @blur="form.default_exchange_rate = formatContractDecimal(form.default_exchange_rate)"
            inputmode="decimal" pattern="[0-9]+([.][0-9]{1,10})?" class="input mt-1" />
        </label>
      </fieldset>
      <div class="flex justify-end gap-2">
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="busy"
          @click="emit('close')"
        >
          {{ t('vendor.cancel') }}</button
        ><button
          class="btn btn-primary"
          :disabled="
            busy ||
            !form.platforms.length ||
            !form.group_ids.length ||
            !validOpenAIModes ||
            (form.tier === 2 && !selectedParent)
          "
        >
          {{ t(busy ? 'vendor.loading' : 'vendor.save') }}
        </button>
      </div>
    </form></BaseDialog
  >
</template>
