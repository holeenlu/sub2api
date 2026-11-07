<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type CnApiProtocol } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'


const props = defineProps<{
  isMultiProtocolPlatform: UnwrapRef<ComputedRef<boolean>>
  cnProtocolOptions: UnwrapRef<ComputedRef<{ value: CnApiProtocol; labelKey: string }[]>>
  apiProtocol: UnwrapRef<Ref<CnApiProtocol, CnApiProtocol>>
  cnAccentActiveClass: UnwrapRef<ComputedRef<"border-pink-500 bg-pink-50 dark:bg-pink-900/20" | "border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20" | "border-teal-500 bg-teal-50 dark:bg-teal-900/20" | "border-rose-500 bg-rose-50 dark:bg-rose-900/20" | "border-amber-500 bg-amber-50 dark:bg-amber-900/20" | "border-primary-500 bg-primary-50 dark:bg-primary-900/20">>
  cnAccentIconClass: UnwrapRef<ComputedRef<"bg-pink-500 text-white" | "bg-indigo-500 text-white" | "bg-teal-500 text-white" | "bg-rose-500 text-white" | "bg-amber-500 text-white" | "bg-primary-500 text-white">>
}>()

const emit = defineEmits<{
  'update:apiProtocol': [value: UnwrapRef<Ref<CnApiProtocol, CnApiProtocol>>]
}>()

const { t } = useI18n()

const isMultiProtocolPlatform = computed(() => props.isMultiProtocolPlatform)

const cnProtocolOptions = computed(() => props.cnProtocolOptions)

const apiProtocol = usePanelField(() => props.apiProtocol, value => emit('update:apiProtocol', value))

const cnAccentActiveClass = computed(() => props.cnAccentActiveClass)

const cnAccentIconClass = computed(() => props.cnAccentIconClass)
</script>

<template>
<div v-if="isMultiProtocolPlatform" class="mt-4">
        <label class="input-label">{{ t('admin.accounts.cnProviders.apiProtocol.title') }}</label>
        <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <button
            v-for="opt in cnProtocolOptions"
            :key="opt.value"
            type="button"
            @click="apiProtocol = opt.value"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              apiProtocol === opt.value
                ? cnAccentActiveClass
                : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                apiProtocol === opt.value
                  ? cnAccentIconClass
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon :name="opt.value === 'adaptive' ? 'swap' : opt.value === 'anthropic' ? 'sparkles' : opt.value === 'responses' ? 'terminal' : 'chat'" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t(`admin.accounts.cnProviders.apiProtocol.${opt.labelKey}`) }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.cnProviders.apiProtocol.${opt.labelKey}Desc`) }}</span>
            </div>
          </button>
        </div>
      </div>
</template>
