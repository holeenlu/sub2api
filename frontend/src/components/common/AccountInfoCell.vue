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
  proxyName?: string | null
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
    <HelpTooltip class="!ml-0 w-full" trigger="both" width-class="w-72 max-w-[calc(100vw-16px)]" tone="brand">
      <template #trigger="{ expanded }">
        <button type="button" :aria-expanded="expanded" class="flex w-full min-w-0 items-center gap-1.5 text-left leading-5">
          <PlatformIcon :platform="platform" size="xs" class="shrink-0" />
          <span class="min-w-0 truncate font-semibold">{{ name }}</span>
          <span class="shrink-0 text-[11px] font-normal text-gray-400">#{{ id }}</span>
        </button>
      </template>
      <div class="space-y-2 whitespace-normal font-normal" data-account-info>
        <p class="break-all font-semibold">{{ name }} <span>#{{ id }}</span></p>
        <p v-if="email" class="break-all">{{ t('common.email') }}：{{ email }}</p>
        <PlatformTypeBadge v-bind="badgeProps" :summary="false" />
        <p class="break-all">{{ t('admin.accounts.columns.proxy') }}：{{ proxyName || t('vendor.direct') }}</p>
        <a v-if="homepage" :href="homepage" target="_blank" rel="noopener noreferrer" class="block break-all underline">{{ homepage }}</a>
        <slot name="details" />
        <button type="button" class="underline" @click.stop="$emit('copy')">{{ t('vendor.copy') }}</button>
      </div>
    </HelpTooltip>
    <div class="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs leading-4">
      <PlatformTypeBadge v-bind="badgeProps" summary class="shrink-0" />
      <span class="flex min-w-0 items-center gap-1 text-gray-500"><Icon name="globe" size="xs" class="shrink-0" /><span class="truncate">{{ proxyName || t('vendor.direct') }}</span></span>
    </div>
    <slot />
  </div>
</template>
