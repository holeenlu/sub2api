<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { onClickOutside, useColorMode } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
// Same key as the native application; auto follows OS changes until overridden.
const mode = useColorMode({ storageKey: 'theme', emitAuto: true })
const open = ref(false)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const choices = [
  { value: 'light', label: 'lightMode', icon: 'sun' },
  { value: 'dark', label: 'darkMode', icon: 'moon' },
  { value: 'auto', label: 'systemMode', icon: 'desktop' },
] as const

onClickOutside(root, () => { open.value = false })
async function toggle() {
  open.value = !open.value
  if (!open.value) return
  await nextTick()
  root.value?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
}
function select(value: 'light' | 'dark' | 'auto') {
  mode.value = value
  open.value = false
  trigger.value?.focus()
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    open.value = false
    trigger.value?.focus()
    event.stopPropagation()
  }
  if (!open.value || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const items = Array.from(root.value?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') || [])
  const current = items.indexOf(document.activeElement as HTMLElement)
  const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
    : (current + (event.key === 'ArrowUp' ? -1 : 1) + items.length) % items.length
  items[index]?.focus()
}
</script>

<template>
  <div ref="root" class="relative" @keydown="onKeydown">
    <button ref="trigger" type="button" class="flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
      :aria-label="t('vendor.displayMode')" :title="t('vendor.displayMode')" aria-haspopup="menu" :aria-expanded="open" @click="toggle">
      <Icon :name="mode === 'auto' ? 'desktop' : mode === 'dark' ? 'moon' : 'sun'" size="sm" />
      <Icon name="chevronDown" size="xs" />
    </button>
    <div v-if="open" role="menu" :aria-label="t('vendor.displayMode')" class="absolute right-0 z-[70] mt-2 w-44 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-700 dark:bg-dark-800">
      <button v-for="choice in choices" :key="choice.value" type="button" role="menuitemradio" :aria-checked="mode === choice.value"
        class="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-gray-700 hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-200 dark:hover:bg-dark-700"
        :class="{ 'bg-primary-50 dark:bg-primary-900/20': mode === choice.value }" @click="select(choice.value)">
        <Icon :name="choice.icon" size="sm" />
        <span>{{ t('vendor.' + choice.label) }}</span>
        <Icon v-if="mode === choice.value" name="check" size="sm" class="ml-auto text-primary-600 dark:text-primary-400" />
      </button>
    </div>
  </div>
</template>
