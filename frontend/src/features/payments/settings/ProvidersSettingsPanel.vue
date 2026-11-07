<script setup lang="ts">
import { toRaw } from 'vue'

import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProviderInstance } from '../api/types'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import PaymentProviderList from '../components/PaymentProviderList.vue'
import PaymentProviderDialog from '../components/PaymentProviderDialog.vue'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { normalizeVisibleMethod } from '@/features/payments/model/paymentFlow'
import { usePaymentsSettingsServices } from './ports'
const sectionReady = ref(false), sectionError = ref(''), commandError = ref('')
const services = usePaymentsSettingsServices()
const query = usePaymentRequest(() => services.scopeKey.value)
const command = usePaymentRequest(() => services.scopeKey.value)

import type { DeepReadonly } from 'vue'
import type { ProvidersDraft } from './providersModel'
const props = defineProps<{ modelValue: DeepReadonly<ProvidersDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ProvidersDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ProvidersDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof ProvidersDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<ProvidersDraft> = {}
  for (const key of Object.keys(value) as (keyof ProvidersDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

const allPaymentTypes = computed(() => [
  { value: "easypay", label: t("payment.methods.easypay") },
  { value: "alipay", label: t("payment.methods.alipay") },
  { value: "wxpay", label: t("payment.methods.wxpay") },
  { value: "stripe", label: t("payment.methods.stripe") },
  { value: "airwallex", label: t("payment.methods.airwallex") },
]);

const hasAnyPaymentTypeEnabled = computed(
  () => form.payment_enabled_types.length > 0,
);

const providersLoading = ref(false);

const providerSaving = ref(false)
const commandUnknown = ref(false)
const sectionSaving = computed(() => providerSaving.value || commandUnknown.value)

const providers = ref<ProviderInstance[]>([]);

const showProviderDialog = ref(false);

const showDeleteProviderDialog = ref(false);

const editingProvider = ref<ProviderInstance | null>(null);

const deletingProviderId = ref<number | null>(null);

const providerDialogRef = ref<InstanceType<
  typeof PaymentProviderDialog
> | null>(null);

const providerKeyOptions = computed(() => [
  { value: "easypay", label: t("admin.settings.payment.providerEasypay") },
  { value: "alipay", label: t("admin.settings.payment.providerAlipay") },
  { value: "wxpay", label: t("admin.settings.payment.providerWxpay") },
  { value: "stripe", label: t("admin.settings.payment.providerStripe") },
  { value: "airwallex", label: t("admin.settings.payment.providerAirwallex") },
]);

const enabledProviderKeyOptions = computed(() => {
  const enabled = form.payment_enabled_types;
  return providerKeyOptions.value.filter((opt) => enabled.includes(opt.value));
});

type ProviderEnablementCandidate = Pick<
  ProviderInstance,
  "id" | "provider_key" | "supported_types" | "enabled" | "name"
>;

function getProviderVisibleMethods(
  provider: ProviderEnablementCandidate,
): Array<"alipay" | "wxpay"> {
  if (!provider.enabled) {
    return [];
  }

  const supportedTypes = Array.isArray(provider.supported_types)
    ? provider.supported_types
    : [];
  const methods = new Set<"alipay" | "wxpay">();
  const addMethod = (type: string) => {
    const method = normalizeVisibleMethod(type);
    if (method === "alipay" || method === "wxpay") {
      methods.add(method);
    }
  };

  if (provider.provider_key === "alipay") {
    if (supportedTypes.length === 0) {
      methods.add("alipay");
    } else {
      supportedTypes.forEach((type) => {
        if (normalizeVisibleMethod(type) === "alipay") {
          methods.add("alipay");
        }
      });
    }
  } else if (provider.provider_key === "wxpay") {
    if (supportedTypes.length === 0) {
      methods.add("wxpay");
    } else {
      supportedTypes.forEach((type) => {
        if (normalizeVisibleMethod(type) === "wxpay") {
          methods.add("wxpay");
        }
      });
    }
  } else if (provider.provider_key === "easypay") {
    supportedTypes.forEach(addMethod);
  }

  return Array.from(methods);
}

function findProviderEnablementConflict(
  candidate: ProviderEnablementCandidate,
): { method: "alipay" | "wxpay"; conflicting: ProviderInstance } | null {
  const claimedMethods = getProviderVisibleMethods(candidate);
  if (claimedMethods.length === 0) {
    return null;
  }

  for (const other of providers.value) {
    if (other.id === candidate.id || !other.enabled) {
      continue;
    }

    const otherMethods = getProviderVisibleMethods(other);
    const matchedMethod = claimedMethods.find((method) =>
      otherMethods.includes(method),
    );
    if (matchedMethod) {
      return {
        method: matchedMethod,
        conflicting: other,
      };
    }
  }

  return null;
}

function showProviderEnablementConflict(
  conflict: { method: "alipay" | "wxpay"; conflicting: ProviderInstance },
) {
  services.notifications.showError(
    t("admin.settings.payment.enableConflict", {
      method: t(`payment.methods.${conflict.method}`),
      provider: conflict.conflicting.name,
    }),
  );
}

async function loadProviders(): Promise<void> {
  const ticket = query.begin()
  if (!ticket.isCurrent()) return
  sectionReady.value = false
  sectionError.value = ''
  providersLoading.value = true
  try {
    const res = await services.payment.getProviders(ticket)
    if (!ticket.isCurrent()) return
    // Go nil supported_types is an empty collection, not a missing provider.
    providers.value = (res.data || []).map(provider => ({
      ...provider, supported_types: Array.isArray(provider.supported_types) ? provider.supported_types : [],
    }))
    sectionReady.value = true
  } catch (error) {
    if (!ticket.isCurrent()) return
    sectionError.value = t('admin.settings.failedToLoad')
    services.notifications.showError(extractPaymentErrorMessage(error, t, t('common.error')))
  } finally { if (ticket.isCurrent()) providersLoading.value = false }
}

type ProviderTicket = ReturnType<typeof command.begin>
async function runProviderCommand(send: (ticket: ProviderTicket) => Promise<unknown>, onSuccess?: () => void): Promise<void> {
  if (!sectionReady.value || sectionSaving.value) return
  const ticket = command.begin()
  if (!ticket.isCurrent()) return
  query.cancel()
  providersLoading.value = false
  providerSaving.value = true
  commandError.value = ''
  try {
    await send(ticket)
    if (!ticket.isCurrent()) return
    await loadProviders()
    if (ticket.isCurrent()) onSuccess?.()
  } catch (error) {
    if (!ticket.isCurrent()) return
    commandUnknown.value = paymentSubmissionMayBeUnknown(error)
    commandError.value = extractPaymentErrorMessage(error, t, t('common.error'))
    services.notifications.showError(commandError.value)
    // A read may reconcile a partial batch. It never retries a command or proves it was not applied.
    await loadProviders()
  } finally { if (ticket.isCurrent()) providerSaving.value = false }
}

function openCreateProvider(): void {
  if (!sectionReady.value || sectionSaving.value) return
  editingProvider.value = null;
  providerDialogRef.value?.reset(
    enabledProviderKeyOptions.value[0]?.value || "easypay",
  );
  showProviderDialog.value = true;
}

function openEditProvider(provider: ProviderInstance): void {
  if (!sectionReady.value || sectionSaving.value) return
  editingProvider.value = provider;
  providerDialogRef.value?.loadProvider(provider);
  showProviderDialog.value = true;
}

async function handleSaveProvider(payload: Partial<ProviderInstance>): Promise<void> {
  if (sectionSaving.value) return
  const editing = editingProvider.value
  const candidate: ProviderEnablementCandidate = {
    id: editing?.id ?? 0,
    provider_key: payload.provider_key ?? editing?.provider_key ?? '',
    supported_types: payload.supported_types ?? editing?.supported_types ?? [],
    enabled: payload.enabled ?? editing?.enabled ?? false,
    name: payload.name ?? editing?.name ?? '',
  }
  const conflict = findProviderEnablementConflict(candidate)
  if (conflict) { showProviderEnablementConflict(conflict); return }
  await runProviderCommand(
    ticket => editing ? services.payment.updateProvider(editing.id, payload, ticket) : services.payment.createProvider(payload, ticket),
    () => { showProviderDialog.value = false; services.notifications.showSuccess(t('common.saved')) },
  )
}

async function handleToggleField(
  provider: ProviderInstance,
  field: "enabled" | "refund_enabled" | "allow_user_refund",
): Promise<void> {
  if (sectionSaving.value) return
  let newValue: boolean;
  if (field === "enabled") newValue = !provider.enabled;
  else if (field === "refund_enabled") newValue = !provider.refund_enabled;
  else newValue = !provider.allow_user_refund;

  if (field === "enabled" && newValue) {
    const conflict = findProviderEnablementConflict({
      id: provider.id,
      provider_key: provider.provider_key,
      supported_types: provider.supported_types,
      enabled: true,
      name: provider.name,
    });
    if (conflict) {
      showProviderEnablementConflict(conflict);
      return;
    }
  }

  const payload: Record<string, boolean> = { [field]: newValue };
  // Cascade: turning off refund_enabled also turns off allow_user_refund
  if (field === "refund_enabled" && !newValue) {
    payload.allow_user_refund = false;
  }
  await runProviderCommand(ticket => services.payment.updateProvider(provider.id, payload, ticket))
}

async function handleToggleType(provider: ProviderInstance, type: string): Promise<void> {
  if (sectionSaving.value) return
  const currentTypes = Array.isArray(provider.supported_types)
    ? provider.supported_types
    : [];
  const updated = currentTypes.includes(type)
    ? currentTypes.filter((t) => t !== type)
    : [...currentTypes, type];
  const conflict = findProviderEnablementConflict({
    id: provider.id,
    provider_key: provider.provider_key,
    supported_types: updated,
    enabled: provider.enabled,
    name: provider.name,
  });
  if (conflict) {
    showProviderEnablementConflict(conflict);
    return;
  }
  await runProviderCommand(ticket => services.payment.updateProvider(provider.id, { supported_types: updated }, ticket))
}

function confirmDeleteProvider(provider: ProviderInstance): void {
  if (!sectionReady.value || sectionSaving.value) return
  deletingProviderId.value = provider.id;
  showDeleteProviderDialog.value = true;
}

async function handleReorderProviders(updates: { id: number; sort_order: number }[]): Promise<void> {
  await runProviderCommand(async ticket => {
    const results = await Promise.allSettled(updates.map(async update =>
      services.payment.updateProvider(update.id, { sort_order: update.sort_order }, ticket)))
    const failures = results.filter((result): result is PromiseRejectedResult => result.status === 'rejected')
    // Wait for every write before reconciliation; any ambiguous result blocks resubmission.
    const failure = failures.find(result => paymentSubmissionMayBeUnknown(result.reason)) ?? failures[0]
    if (failure) throw failure.reason
  })
}

async function handleDeleteProvider(): Promise<void> {
  const id = deletingProviderId.value
  if (!id) return
  await runProviderCommand(ticket => services.payment.deleteProvider(id, ticket), () => {
    services.notifications.showSuccess(t('common.deleted'))
    showDeleteProviderDialog.value = false
    deletingProviderId.value = null
  })
}
watch(services.scopeKey, () => {
  query.cancel(); command.cancel()
  providers.value = []; providersLoading.value = false; providerSaving.value = false
  sectionReady.value = false; sectionError.value = ''; commandError.value = ''; commandUnknown.value = false
  editingProvider.value = null; deletingProviderId.value = null
  showProviderDialog.value = false; showDeleteProviderDialog.value = false
  providerDialogRef.value?.reset('')
  if (services.scopeKey.value !== null) void loadProviders()
}, { flush: 'sync' })
onMounted(loadProviders)
</script>

<template><section class="space-y-3">
<p v-if="sectionError" role="alert" class="text-sm text-red-600">{{ sectionError }}</p>
<p v-if="commandUnknown" role="status" class="text-sm text-amber-700 dark:text-amber-300">{{ t('common.save') }}: {{ t('common.unknown') }}</p>
<p v-if="commandError" role="alert" class="text-sm text-red-600">{{ commandError }}</p>
<button v-if="!sectionReady || commandUnknown" type="button" :disabled="providersLoading || providerSaving" class="btn btn-secondary" @click="loadProviders">{{ t('common.tryAgain') }}</button>
<fieldset :disabled="!sectionReady || sectionSaving">

<PaymentProviderList
            v-if="form.payment_enabled"
            :providers="providers"
            :loading="providersLoading"
            :can-create="hasAnyPaymentTypeEnabled"
            :enabled-payment-types="form.payment_enabled_types"
            :all-payment-types="allPaymentTypes"
            :redirect-label="t('admin.settings.payment.easypayRedirect')"
            @refresh="loadProviders"
            @create="openCreateProvider"
            @edit="openEditProvider"
            @delete="confirmDeleteProvider"
            @toggle-field="handleToggleField"
            @toggle-type="handleToggleType"
            @reorder="handleReorderProviders"
          />
<PaymentProviderDialog
        ref="providerDialogRef"
        :show="showProviderDialog"
        :saving="sectionSaving"
        :editing="editingProvider"
        :all-key-options="providerKeyOptions"
        :enabled-key-options="enabledProviderKeyOptions"
        :all-payment-types="allPaymentTypes"
        :redirect-label="t('admin.settings.payment.easypayRedirect')"
        @close="showProviderDialog = false"
        @save="handleSaveProvider"
      />
<ConfirmDialog
        :show="showDeleteProviderDialog"
        :title="t('admin.settings.payment.deleteProvider')"
        :message="t('admin.settings.payment.deleteProviderConfirm')"
        :confirm-text="sectionSaving ? t('common.processing') : t('common.delete')"
        danger
        @confirm="handleDeleteProvider"
        @cancel="showDeleteProviderDialog = false"
      />

</fieldset>
</section></template>
