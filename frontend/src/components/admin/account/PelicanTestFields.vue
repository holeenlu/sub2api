<template>
  <div class="space-y-3">
    <TextArea :model-value="modelValue.prompt" :label="t('admin.accounts.pelicanTest.promptLabel')" :rows="3" @update:model-value="update('prompt', $event)" />
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="mb-1 block text-xs text-gray-600 dark:text-gray-400">{{ t('admin.accounts.pelicanTest.reasoning') }}</label>
        <Select :model-value="modelValue.reasoning_effort" :options="reasoningOptions" @update:model-value="update('reasoning_effort', $event)" />
      </div>
      <Input :model-value="String(modelValue.parallel_count)" type="number" :label="t('admin.accounts.pelicanTest.parallel')" :hint="t('admin.accounts.pelicanTest.parallelHint')" @update:model-value="update('parallel_count', Number($event))" />
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import type { PelicanTestConfig } from '@/types'
const props = defineProps<{ modelValue: PelicanTestConfig }>()
const emit = defineEmits<{ 'update:modelValue': [value: PelicanTestConfig] }>()
const { t } = useI18n()
const reasoningOptions = computed(() => ['low', 'medium', 'high'].map(value => ({ value, label: t(`admin.accounts.pelicanTest.reasoning${value[0].toUpperCase()}${value.slice(1)}`) })))
function update(key: keyof PelicanTestConfig, value: string | number | boolean | null) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
</script>
