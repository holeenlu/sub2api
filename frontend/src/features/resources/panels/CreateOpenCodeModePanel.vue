<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type OpenCodeAccountMode } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'


const props = defineProps<{
  isOpenCodeGoPlatform: UnwrapRef<ComputedRef<boolean>>
  openCodeAccountMode: UnwrapRef<Ref<OpenCodeAccountMode, OpenCodeAccountMode>>
  cnAccentActiveClass: UnwrapRef<ComputedRef<"border-pink-500 bg-pink-50 dark:bg-pink-900/20" | "border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20" | "border-teal-500 bg-teal-50 dark:bg-teal-900/20" | "border-rose-500 bg-rose-50 dark:bg-rose-900/20" | "border-amber-500 bg-amber-50 dark:bg-amber-900/20" | "border-primary-500 bg-primary-50 dark:bg-primary-900/20">>
  cnAccentIconClass: UnwrapRef<ComputedRef<"bg-pink-500 text-white" | "bg-indigo-500 text-white" | "bg-teal-500 text-white" | "bg-rose-500 text-white" | "bg-amber-500 text-white" | "bg-primary-500 text-white">>
}>()

const emit = defineEmits<{
  'update:openCodeAccountMode': [value: UnwrapRef<Ref<OpenCodeAccountMode, OpenCodeAccountMode>>]
}>()

const { t } = useI18n()

const isOpenCodeGoPlatform = computed(() => props.isOpenCodeGoPlatform)

const openCodeAccountMode = usePanelField(() => props.openCodeAccountMode, value => emit('update:openCodeAccountMode', value))

const cnAccentActiveClass = computed(() => props.cnAccentActiveClass)

const cnAccentIconClass = computed(() => props.cnAccentIconClass)
</script>

<template>
<div v-if="isOpenCodeGoPlatform">
        <label class="input-label">{{ t('admin.accounts.cnProviders.accountMode.title') }}</label>
        <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <button
            type="button"
            @click="openCodeAccountMode = 'zen'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              openCodeAccountMode === 'zen'
                ? cnAccentActiveClass
                : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                openCodeAccountMode === 'zen' ? cnAccentIconClass : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="creditCard" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.opencodeGo.accountMode.zen') }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.accountMode.zenDesc') }}</span>
            </div>
          </button>
          <button
            type="button"
            @click="openCodeAccountMode = 'go'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              openCodeAccountMode === 'go'
                ? cnAccentActiveClass
                : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                openCodeAccountMode === 'go' ? cnAccentIconClass : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="bolt" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.opencodeGo.accountMode.go') }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.accountMode.goDesc') }}</span>
            </div>
          </button>
        </div>
      </div>
</template>
