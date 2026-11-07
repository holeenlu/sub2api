<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Select from '@/shared/ui/Select.vue'
import { useBulkKeyEdit, type BulkKeyEditProps } from '../model/useBulkKeyEdit'
const props = defineProps<BulkKeyEditProps>()
const emit = defineEmits<{ close: []; updated: [succeededIds: number[]] }>()
const { t } = useI18n()
const { submitting, pendingKeys, failures, enabled, groupId, status, limits, ipLists, neverExpires,
  expirationDate, limitFields, ipFields, groupOptions, statusOptions, validationError, canSubmit, close, submit } = useBulkKeyEdit(props, emit)
</script>
<template>
  <BaseDialog
    :show="show"
    :title="t('keys.bulkEdit.title')"
    width="normal"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="close"
  >
    <form id="bulk-edit-keys-form" class="space-y-5" @submit.prevent="submit">
      <div class="space-y-1 text-sm">
        <p class="font-medium text-gray-900 dark:text-white">
          {{ t('keys.bulkEdit.selectedCount', { count: pendingKeys.length }) }}
        </p>
        <p class="text-gray-500 dark:text-gray-400">{{ t('keys.bulkEdit.hint') }}</p>
      </div>

      <fieldset :disabled="submitting" class="space-y-5">
        <div class="space-y-2">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input v-model="enabled.group_id" type="checkbox" class="checkbox" data-test="enable-group" />
            {{ t('keys.groupLabel') }}
          </label>
          <Select
            v-if="enabled.group_id"
            v-model="groupId"
            :options="groupOptions"
            :placeholder="t('keys.selectGroup')"
            :disabled="submitting"
            :aria-label="t('keys.groupLabel')"
            searchable
            data-test="group-input"
          />
        </div>

        <div class="space-y-2">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input v-model="enabled.status" type="checkbox" class="checkbox" data-test="enable-status" />
            {{ t('keys.statusLabel') }}
          </label>
          <Select
            v-if="enabled.status"
            v-model="status"
            :options="statusOptions"
            :disabled="submitting"
            :aria-label="t('keys.statusLabel')"
            data-test="status-input"
          />
        </div>

        <div v-for="field in limitFields" :key="field.key" class="space-y-2">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input
              v-model="enabled[field.key]"
              type="checkbox"
              class="checkbox"
              :data-test="`enable-${field.key}`"
            />
            {{ t(field.label) }}
          </label>
          <div v-if="enabled[field.key]">
            <input
              v-model="limits[field.key]"
              type="number"
              min="0"
              step="any"
              required
              class="input"
              :aria-label="t(field.label)"
              :data-test="`${field.key}-input`"
            />
            <p class="input-hint">{{ t('keys.bulkEdit.limitHint') }}</p>
          </div>
        </div>

        <div class="space-y-2">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input v-model="enabled.expires_at" type="checkbox" class="checkbox" data-test="enable-expiration" />
            {{ t('keys.expiration') }}
          </label>
          <div v-if="enabled.expires_at" class="space-y-2">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="neverExpires" type="checkbox" class="checkbox" data-test="never-expires" />
              {{ t('keys.noExpiration') }}
            </label>
            <input
              v-if="!neverExpires"
              v-model="expirationDate"
              type="datetime-local"
              required
              class="input"
              :aria-label="t('keys.expirationDate')"
              data-test="expiration-input"
            />
          </div>
        </div>

        <div v-for="field in ipFields" :key="field.key" class="space-y-2">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input
              v-model="enabled[field.key]"
              type="checkbox"
              class="checkbox"
              :data-test="`enable-${field.key}`"
            />
            {{ t(field.label) }}
          </label>
          <div v-if="enabled[field.key]">
            <textarea
              v-model="ipLists[field.key]"
              rows="3"
              class="input font-mono text-sm"
              :aria-label="t(field.label)"
              :data-test="`${field.key}-input`"
            />
            <p class="input-hint">{{ t('keys.bulkEdit.ipHint') }}</p>
          </div>
        </div>
      </fieldset>

      <p v-if="validationError" role="alert" class="text-sm text-red-600 dark:text-red-400">
        {{ validationError }}
      </p>
      <div v-if="failures.length" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
        <p>{{ t('keys.bulkEdit.failureHint') }}</p>
        <ul class="max-h-40 space-y-1 overflow-y-auto">
          <li v-for="failure in failures" :key="failure.id" class="break-words">
            #{{ failure.id }} {{ failure.name }}: {{ failure.message }}
          </li>
        </ul>
      </div>
    </form>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">
        {{ t('common.cancel') }}
      </button>
      <button
        type="submit"
        form="bulk-edit-keys-form"
        class="btn btn-primary"
        :disabled="!canSubmit"
        data-test="submit"
      >
        {{ submitting ? t('keys.saving') : t('keys.bulkEdit.apply', { count: pendingKeys.length }) }}
      </button>
    </template>
  </BaseDialog>
</template>

