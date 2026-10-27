<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformTypeBadge, { type AccountBadgeProps } from './PlatformTypeBadge.vue'
import PlatformIcon from './PlatformIcon.vue'
import HelpTooltip from './HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
interface Props extends AccountBadgeProps {
  name: string
  id: number
  email?: string | null
  homepage?: string
}
const props = defineProps<Props>()
const { t } = useI18n()
const badgeProps = computed(() => ({
  platform: props.platform, type: props.type, authMode: props.authMode, planType: props.planType,
  privacyMode: props.privacyMode, subscriptionExpiresAt: props.subscriptionExpiresAt,
  method: props.method, billingType: props.billingType, authorizationModes: props.authorizationModes, compact: props.compact,
}))
defineEmits<{ copy: [] }>()
</script>

<template>
  <div class="w-[236px] max-w-full min-w-0" data-account-summary>
    <HelpTooltip class="!ml-0 w-full" trigger="both" width-class="w-72 max-w-[calc(100vw-16px)]" tone="surface">
      <template #trigger="{ expanded }">
        <div class="flex w-full min-w-0 items-center gap-1.5 leading-5">
          <PlatformIcon :platform="platform" size="xs" class="shrink-0" />
          <button type="button" data-account-copy :aria-label="t('vendor.copy') + ' ' + (email || name)" class="min-w-0 truncate text-left font-semibold hover:text-primary-600 hover:underline" @click.stop="$emit('copy')">{{ name }}</button>
          <span class="shrink-0 text-[11px] font-normal text-gray-400">#{{ id }}</span>
          <button type="button" data-account-details :aria-label="t('common.info') + ' ' + name" :aria-expanded="expanded" class="inline-flex shrink-0 items-center justify-center rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700">
            <Icon name="infoCircle" size="xs" />
          </button>
        </div>
      </template>
      <div class="select-text whitespace-normal font-normal" data-account-info>
        <div class="mb-2 flex items-start gap-3 border-b border-gray-200 pb-2 dark:border-dark-600">
          <p class="min-w-0 flex-1 break-all font-medium text-gray-900 dark:text-gray-100">{{ name }} <span class="font-normal text-gray-500">#{{ id }}</span></p>
          <button type="button" class="shrink-0 rounded px-1.5 py-0.5 text-primary-700 hover:bg-gray-100 dark:text-primary-300 dark:hover:bg-dark-700" @click.stop="$emit('copy')">{{ t('vendor.copy') }}</button>
        </div>
        <PlatformTypeBadge v-bind="badgeProps">
          <template #default="{ platformLabel, typeLabel, planLabel, expiresLabel, authorizationLabel, privacyBadge, compactMeta, compactTitle }">
            <div class="space-y-2">
              <dl class="account-detail-grid">
                <template v-if="email"><dt class="text-gray-500 dark:text-gray-400">{{ t('common.email') }}</dt><dd>{{ email }}</dd></template>
                <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.platform') }}</dt><dd>{{ platformLabel }} · {{ typeLabel }}</dd>
              </dl>
              <dl v-if="planLabel || billingType || expiresLabel" class="account-detail-grid border-t border-gray-100 pt-2 dark:border-dark-700">
                <template v-if="planLabel"><dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.accountPlanLabel') }}</dt><dd>{{ planLabel }}</dd></template>
                <template v-if="billingType"><dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.billingType') }}</dt><dd>{{ billingType }}</dd></template>
                <template v-if="expiresLabel"><dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.subscriptionExpiresAt') }}</dt><dd>{{ expiresLabel.replace(t('admin.accounts.subscriptionExpires') + ' ', '') }}</dd></template>
              </dl>
              <dl v-if="authorizationLabel || privacyBadge || compactMeta" class="account-detail-grid border-t border-gray-100 pt-2 dark:border-dark-700">
                <template v-if="authorizationLabel"><dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.method') }}</dt><dd>{{ authorizationLabel }}</dd></template>
                <template v-if="privacyBadge"><dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.privacyStatus') }}</dt><dd>{{ privacyBadge.title }}</dd></template>
                <template v-if="compactMeta"><dt class="text-gray-500 dark:text-gray-400">Compact</dt><dd :class="compactMeta.className" :title="compactTitle">{{ compactMeta.label.replace(/Compact\s*/, '').trim() }}</dd></template>
              </dl>
            </div>
          </template>
        </PlatformTypeBadge>
        <a v-if="homepage" :href="homepage" target="_blank" rel="noopener noreferrer" class="mt-2 block break-all text-primary-700 underline dark:text-primary-300">{{ homepage }}</a>
        <slot name="details" />
      </div>
    </HelpTooltip>
    <div class="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs leading-4">
      <PlatformTypeBadge v-bind="badgeProps" summary class="shrink-0" />
    </div>
    <slot />
  </div>
</template>

<style scoped>
.account-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 2fr); gap: 4px 16px; align-items: baseline; }
.account-detail-grid dd { min-width: 0; overflow-wrap: anywhere; }
@media (pointer: coarse) { [data-account-details] { min-width: 32px; min-height: 32px; } }
</style>
