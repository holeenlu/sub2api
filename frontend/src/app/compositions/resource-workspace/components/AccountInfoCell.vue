<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformTypeBadge, { type AccountBadgeProps } from './PlatformTypeBadge.vue'
import PlatformIcon from '@/shared/ui/PlatformIcon.vue'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { getIntlLocale } from '@/i18n/localeUtils'
import { credentialExpiryDate, paymentMethodLabel, recordedAccountString } from '../model/accountInfo'
interface Props extends AccountBadgeProps {
  name: string
  id: number
  email?: string | null
  homepage?: string
  credentialExpiresAt?: unknown
  rateLimitTier?: string
  subscriptionType?: string
}
const props = defineProps<Props>()
const { t, locale } = useI18n()
const unknown = computed(() => t('admin.accounts.identitySummary.unknown'))
const expiry = computed(() => credentialExpiryDate(props.credentialExpiresAt))
const expiryShort = computed(() => expiry.value?.toLocaleDateString(getIntlLocale(locale.value), { year: 'numeric', month: '2-digit', day: '2-digit' }) || unknown.value)
const expiryFull = computed(() => expiry.value?.toLocaleString(getIntlLocale(locale.value), { timeZoneName: 'short' }) || unknown.value)
const payment = computed(() => paymentMethodLabel(props.billingType, props.subscriptionType, t))
const tier = computed(() => recordedAccountString(props.rateLimitTier))
const badgeProps = computed(() => ({
  platform: props.platform, type: props.type, authMode: props.authMode, planType: props.planType,
  privacyMode: props.privacyMode, subscriptionExpiresAt: props.subscriptionExpiresAt,
  method: props.method, billingType: props.billingType, authorizationModes: props.authorizationModes, compact: props.compact,
}))
defineEmits<{ copy: [] }>()
</script>

<template>
  <div class="w-[236px] max-w-full min-w-0" data-account-summary>
    <HelpTooltip class="!ml-0 w-full" trigger="both" width-class="w-96 max-w-[calc(100vw-16px)]" tone="surface">
      <template #trigger="{ expanded, tooltipId }">
        <div class="w-full min-w-0">
          <div class="flex w-full min-w-0 items-center gap-1.5 leading-5" data-account-identity-row>
            <PlatformIcon :platform="platform" size="xs" class="shrink-0" />
            <button type="button" data-account-copy :aria-label="t('vendor.copy') + ' ' + (email || name)" class="min-w-0 truncate rounded text-left font-semibold hover:text-primary-600 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" @click.stop="$emit('copy')">{{ name }}</button>
            <span class="shrink-0 text-[11px] font-normal text-gray-400">#{{ id }}</span>
            <button type="button" data-account-details :aria-label="t('common.info') + ' ' + name" :aria-expanded="expanded" :aria-controls="tooltipId" class="inline-flex shrink-0 items-center justify-center rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-primary-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:bg-dark-700">
              <Icon name="infoCircle" size="xs" />
            </button>
          </div>
          <PlatformTypeBadge v-bind="badgeProps" summary>
            <template #default="{ planLabel }">
              <div class="account-summary-metadata mt-0.5 text-xs leading-4" data-account-metadata-row>
                <span class="account-summary-chip" data-account-plan :aria-label="t('vendor.accountPlanLabel') + ': ' + (planLabel || unknown)">{{ planLabel || unknown }}</span>
                <span class="account-summary-chip" data-account-payment :aria-label="t('admin.accounts.identitySummary.paymentOrSubscription') + ': ' + (payment || unknown)">{{ payment || unknown }}</span>
                <span class="account-summary-chip text-[10px]" data-account-credential-expiry :aria-label="t('admin.accounts.identitySummary.credentialExpiry') + ': ' + expiryFull">
                  <time v-if="expiry" :datetime="expiry.toISOString()">{{ expiryShort }}</time><span v-else>{{ unknown }}</span>
                </span>
              </div>
            </template>
          </PlatformTypeBadge>
        </div>
      </template>
      <div class="select-text whitespace-normal font-normal" data-account-info>
        <div class="mb-2 flex items-start gap-3 border-b border-gray-200 pb-2 dark:border-dark-600">
          <p class="min-w-0 flex-1 break-all font-medium text-gray-900 dark:text-gray-100">{{ name }} <span class="font-normal text-gray-500">#{{ id }}</span></p>
          <button type="button" class="shrink-0 rounded px-1.5 py-0.5 text-primary-700 hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:text-primary-300 dark:hover:bg-dark-700" @click.stop="$emit('copy')">{{ t('vendor.copy') }}</button>
        </div>
        <PlatformTypeBadge v-bind="badgeProps">
          <template #default="{ platformLabel, typeLabel, planLabel, expiresLabel, authorizationLabel, privacyBadge, compactMeta, compactTitle }">
            <div>
              <dl class="account-detail-grid">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('common.email') }}</dt><dd>{{ email || unknown }}</dd>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.platform') }}</dt><dd>{{ platformLabel }} · {{ typeLabel }}</dd>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.accountPlanLabel') }}</dt><dd data-account-plan-detail>{{ planLabel || unknown }}<span v-if="tier && tier !== planLabel" class="text-gray-500 dark:text-gray-400"> · {{ tier }}</span></dd>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.identitySummary.paymentOrSubscription') }}</dt><dd data-account-payment-detail>{{ payment || unknown }}</dd>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.method') }}</dt><dd data-account-auth-detail>{{ authorizationLabel || typeLabel }}</dd>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.identitySummary.credentialExpiry') }}</dt><dd data-account-expiry-detail><time v-if="expiry" :datetime="expiry.toISOString()">{{ expiryFull }}</time><span v-else>{{ unknown }}</span></dd>
                <template v-if="expiresLabel"><dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.subscriptionExpiresAt') }}</dt><dd>{{ expiresLabel.replace(t('admin.accounts.subscriptionExpires') + ' ', '') }}</dd></template>
                <template v-if="privacyBadge"><dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.privacyStatus', t('common.status')) }}</dt><dd>{{ privacyBadge.title }}</dd></template>
                <template v-if="compactMeta"><dt class="text-gray-500 dark:text-gray-400">Compact</dt><dd :class="compactMeta.className" :title="compactTitle">{{ compactMeta.label.replace(/Compact\s*/, '').trim() }}</dd></template>
              </dl>
            </div>
          </template>
        </PlatformTypeBadge>
        <a v-if="homepage" :href="homepage" target="_blank" rel="noopener noreferrer" class="mt-2 block break-all text-primary-700 underline dark:text-primary-300">{{ homepage }}</a>
        <slot name="details" />
      </div>
    </HelpTooltip>
    <slot />
  </div>
</template>

<style scoped>
.account-detail-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 4px 16px;
  align-items: baseline;
}
.account-detail-grid dd { min-width: 0; overflow-wrap: anywhere; }
.account-summary-metadata { display: grid; grid-template-columns: minmax(0, 0.8fr) minmax(0, 1fr) auto; gap: 4px; }
.account-summary-chip { display: block; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; border-radius: 3px; padding: 1px 4px; background: rgb(243 244 246); color: rgb(75 85 99); }
:global(.dark) .account-summary-chip { background: rgb(55 65 81); color: rgb(209 213 219); }
@media (pointer: coarse) {
  [data-account-details] { min-width: 32px; min-height: 32px; }
}
</style>
