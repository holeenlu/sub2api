<script setup lang="ts">
import { ref } from 'vue'
import { expectTypeOf } from 'vitest'
import Select from '../Select.vue'
import type { CreatableSelectOption, SelectValue } from '../optionTypes'

interface Choice { id: number; title: string; detail: { count: number } }
type SelectedChoice = Choice | null
type OptionChoice = Choice | CreatableSelectOption
const options: readonly Choice[] = [{ id: 0, title: 'Zero', detail: { count: 2 } }]
const value = ref<SelectValue>(0)

function checkSelected(option: Choice | null): string {
  return option?.title ?? 'Choose'
}

function checkOption(option: Choice | CreatableSelectOption): string {
  if ('_creatable' in option) {
    expectTypeOf(option.id).toBeUnknown()
    // A synthetic option has no domain detail, even if all input options do.
    // @ts-expect-error Creatable fields require runtime validation.
    expectTypeOf(option.detail.count).toBeNumber()
    return String(option.title ?? '')
  }
  expectTypeOf(option).toEqualTypeOf<Choice>()
  return `${option.title}: ${option.detail.count}`
}
</script>

<template>
  <Select v-model="value" :options="options" value-key="id" label-key="title" creatable>
    <template #selected="{ option }">
      {{ expectTypeOf(option).toEqualTypeOf<SelectedChoice>() }}
      {{ checkSelected(option) }}
    </template>
    <template #option="{ option, selected }">
      {{ expectTypeOf(option).toEqualTypeOf<OptionChoice>() }}
      {{ expectTypeOf(selected).toEqualTypeOf<boolean>() }}
      {{ checkOption(option) }}
    </template>
  </Select>
</template>
