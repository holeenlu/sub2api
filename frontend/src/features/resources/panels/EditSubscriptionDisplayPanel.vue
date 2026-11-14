<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'
import type { PlanTypeOption } from '../model/credentialsBuilder'


const props = defineProps<{
  account: NonNullable<Props['account']>
  isSparkShadow: UnwrapRef<ComputedRef<boolean>>
  editPlanType: UnwrapRef<Ref<string, string>>
  planTypeOptions: UnwrapRef<ComputedRef<PlanTypeOption[]>>
}>()

const emit = defineEmits<{
  'update:editPlanType': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const isSparkShadow = computed(() => props.isSparkShadow)

const editPlanType = usePanelField(() => props.editPlanType, value => emit('update:editPlanType', value))

const planTypeOptions = computed(() => props.planTypeOptions)
</script>

<template>
<template v-if="account">
<div
        v-if="account?.platform === 'openai' && account?.type === 'oauth' && !isSparkShadow"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.openai.planType') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.planTypeDesc') }}
            </p>
          </div>
          <div class="w-44 flex-shrink-0">
            <Select v-model="editPlanType" :options="planTypeOptions" />
          </div>
        </div>
      </div>
</template>
</template>
