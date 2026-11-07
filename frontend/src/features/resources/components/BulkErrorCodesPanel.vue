<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { commonErrorCodes } from '../model/modelRestrictions'

const props = defineProps<{
  selected: boolean
  codes: readonly number[]
  input: number | string | null
}>()
const emit = defineEmits<{
  'update:selected': [value: boolean]
  'update:codes': [value: number[]]
  'update:input': [value: number | string | null]
  info: [message: string]
  error: [message: string]
}>()
const { t } = useI18n()
const customInput = computed({
  get: () => props.input,
  set: value => { if (props.selected) emit('update:input', value) }
})
const sortedCodes = computed(() => [...props.codes].sort((a, b) => a - b))

function confirmErrorCode(code: number): boolean {
  if (code === 429) return confirm(t('admin.accounts.customErrorCodes429Warning'))
  if (code === 529) return confirm(t('admin.accounts.customErrorCodes529Warning'))
  return true
}

function toggleErrorCode(code: number) {
  if (!props.selected) return
  if (props.codes.includes(code)) removeErrorCode(code)
  else if (confirmErrorCode(code)) emit('update:codes', [...props.codes, code])
}

function addCustomErrorCode() {
  if (!props.selected) return
  const code = props.input
  if (typeof code !== 'number' || !Number.isInteger(code) || code < 100 || code > 599) {
    emit('error', t('admin.accounts.invalidErrorCode'))
    return
  }
  if (props.codes.includes(code)) {
    emit('info', t('admin.accounts.errorCodeExists'))
    return
  }
  if (!confirmErrorCode(code)) return
  emit('update:codes', [...props.codes, code])
  emit('update:input', null)
}

function removeErrorCode(code: number) {
  if (!props.selected) return
  emit('update:codes', props.codes.filter(selectedCode => selectedCode !== code))
}
</script>

<template>
  <!-- Custom error codes -->
  <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="mb-3 flex items-center justify-between">
      <div>
        <label
          id="bulk-edit-custom-error-codes-label"
          class="input-label mb-0"
          for="bulk-edit-custom-error-codes-enabled"
        >
          {{ t('admin.accounts.customErrorCodes') }}
        </label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.customErrorCodesHint') }}
        </p>
      </div>
      <input
        :checked="selected"
        @change="emit('update:selected', !selected)"
        id="bulk-edit-custom-error-codes-enabled"
        type="checkbox"
        aria-controls="bulk-edit-custom-error-codes-body"
        class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
      />
    </div>

    <div v-if="selected" id="bulk-edit-custom-error-codes-body" class="space-y-3">
      <div class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20">
        <p class="text-xs text-amber-700 dark:text-amber-400">
          <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
          {{ t('admin.accounts.customErrorCodesWarning') }}
        </p>
      </div>

      <!-- Error Code Buttons -->
      <div class="flex flex-wrap gap-2">
        <button
          v-for="code in commonErrorCodes"
          :key="code.value"
          type="button"
          :class="[
            'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
            codes.includes(code.value)
              ? 'bg-red-100 text-red-700 ring-1 ring-red-500 dark:bg-red-900/30 dark:text-red-400'
              : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
          ]"
          :data-testid="'bulk-error-code-toggle-' + code.value"
          @click="toggleErrorCode(code.value)"
        >
          {{ code.value }} {{ code.label }}
        </button>
      </div>

      <!-- Manual input -->
      <div class="flex items-center gap-2">
        <input
          v-model.number="customInput"
          id="bulk-edit-custom-error-code-input"
          type="number"
          min="100"
          max="599"
          class="input flex-1"
          :placeholder="t('admin.accounts.enterErrorCode')"
          aria-labelledby="bulk-edit-custom-error-codes-label"
          @keydown.enter.prevent="addCustomErrorCode"
        />
        <button type="button" class="btn btn-secondary px-3" :aria-label="t('common.add')" data-testid="bulk-error-code-add" @click="addCustomErrorCode">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 4v16m8-8H4"
            />
          </svg>
        </button>
      </div>

      <!-- Selected codes summary -->
      <div class="flex flex-wrap gap-1.5">
        <span
          v-for="code in sortedCodes"
          :key="code"
          class="inline-flex items-center gap-1 rounded-full bg-red-100 px-2.5 py-0.5 text-sm font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
        >
          {{ code }}
          <button
            type="button"
            class="hover:text-red-900 dark:hover:text-red-300"
            :aria-label="t('common.delete') + ' ' + code"
            :data-testid="'bulk-error-code-remove-' + code"
            @click="removeErrorCode(code)"
          >
            <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
          </button>
        </span>
        <span v-if="codes.length === 0" class="text-xs text-gray-400">
          {{ t('admin.accounts.noneSelectedUsesDefault') }}
        </span>
      </div>
    </div>
  </div>
</template>
