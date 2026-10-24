<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ loading?: boolean; error?: string; empty?: boolean }>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()
</script>
<template>
  <div v-if="loading" role="status" class="p-10 text-center text-gray-500">
    {{ t('vendor.loading') }}
  </div>
  <div
    v-else-if="error"
    role="alert"
    class="rounded-xl border border-red-200 bg-red-50 p-4 text-red-800 dark:bg-red-950"
  >
    <p class="break-words">{{ error }}</p>
    <button class="btn btn-secondary mt-3" @click="$emit('retry')">
      {{ t('vendor.retry') }}
    </button>
  </div>
  <p v-else-if="empty" class="p-10 text-center text-gray-500">
    {{ t('vendor.empty') }}
  </p>
  <slot v-else />
</template>
