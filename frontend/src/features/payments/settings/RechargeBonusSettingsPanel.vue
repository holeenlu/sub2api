<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import RechargeBonusTierEditor from './RechargeBonusTierEditor.vue'

import type { DeepReadonly } from 'vue'
import type { RechargeBonusDraft } from './rechargeBonusModel'
const props = defineProps<{ modelValue: DeepReadonly<RechargeBonusDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<RechargeBonusDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as RechargeBonusDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof RechargeBonusDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<RechargeBonusDraft> = {}
  for (const key of Object.keys(value) as (keyof RechargeBonusDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })



</script>

<template>
<RechargeBonusTierEditor
            v-if="form.payment_enabled"
            v-model="form.payment_recharge_bonus_tiers"
            v-model:mode="form.payment_recharge_bonus_mode"
            v-model:notice="form.payment_recharge_bonus_notice"
          />
</template>
