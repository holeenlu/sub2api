<template>
  <span
    v-if="visible"
    class="mt-1 inline-flex max-w-full items-center gap-1 self-start rounded-md bg-red-50 px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300"
  >
    {{ t('admin.accounts.policyCapacityUnavailable') }}
  </span>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ until?: string | null }>()
const { t } = useI18n()
const visible = ref(false)
let expiryTimer: ReturnType<typeof setTimeout> | undefined

function clearExpiryTimer() {
  if (expiryTimer !== undefined) {
    clearTimeout(expiryTimer)
    expiryTimer = undefined
  }
}

function scheduleExpiry() {
  clearExpiryTimer()
  const expiresAt = props.until ? Date.parse(props.until) : NaN
  const remaining = expiresAt - Date.now()
  visible.value = Number.isFinite(expiresAt) && remaining > 0
  if (visible.value) {
    // Use the server deadline even without list refresh; cap browser timer overflow.
    expiryTimer = setTimeout(scheduleExpiry, Math.min(remaining, 2 ** 31 - 1))
  }
}

watch(() => props.until, scheduleExpiry, { immediate: true })
onUnmounted(clearExpiryTimer)
</script>
