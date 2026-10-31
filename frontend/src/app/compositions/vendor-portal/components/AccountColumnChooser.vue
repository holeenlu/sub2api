<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import Icon from '@/shared/ui/icons/Icon.vue'
import { getFloatingPanelPosition } from '@/shared/lib/floatingPanel'

export interface AccountColumnOption {
  key: string
  label: string
}

const props = defineProps<{
  columns: AccountColumnOption[]
  hidden: Set<string>
  label: string
  alwaysVisible?: string[]
}>()
const emit = defineEmits<{
  toggle: [key: string]
  reset: []
}>()
const open = ref(false)
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLDivElement>()
const style = ref<Record<string, string>>({})
const id = `vendor-account-columns-${Math.random().toString(36).slice(2, 8)}`
const alwaysVisible = computed(() => new Set(props.alwaysVisible || []))
const visible = computed(() =>
  props.columns.filter((column) => alwaysVisible.value.has(column.key) || !props.hidden.has(column.key)),
)
const isAlwaysVisible = (key: string) => alwaysVisible.value.has(key)
function toggleColumn(key: string) {
  if (isAlwaysVisible(key)) return
  emit('toggle', key)
}

async function toggle() {
  if (open.value) {
    open.value = false
    return
  }
  if (!trigger.value) return
  const position = getFloatingPanelPosition(
    trigger.value.getBoundingClientRect(),
    window.innerWidth,
    window.innerHeight,
    { maxWidth: 250 },
  )
  style.value = {
    top: `${position.top ?? 0}px`,
    bottom: position.bottom == null ? 'auto' : `${position.bottom}px`,
    left: `${position.left}px`,
    width: '250px',
  }
  open.value = true
  await nextTick()
  panel.value?.querySelector<HTMLButtonElement>('button')?.focus()
}
function close() {
  open.value = false
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.stopPropagation()
    close()
  }
}
onClickOutside(panel, close, { ignore: [trigger] })
useEventListener(window, 'resize', close)
useEventListener(window, 'scroll', close, { capture: true })
onUnmounted(close)
</script>

<template>
  <button
    ref="trigger"
    type="button"
    class="btn btn-secondary btn-sm"
    :aria-label="label"
    :aria-expanded="open"
    :aria-controls="id"
    aria-haspopup="menu"
    @click="toggle"
    @keydown.esc="close"
  >
    <Icon name="cog" size="sm" />{{ label }}
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      :id="id"
      ref="panel"
      role="menu"
      :aria-label="label"
      :style="style"
      class="fixed z-[100] overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-600 dark:bg-dark-800"
      @keydown="keydown"
    >
      <div class="border-b border-gray-100 px-3 py-2 text-sm font-semibold dark:border-dark-700">
        {{ $t('vendor.visibleColumns') }}
      </div>
      <button
        v-for="column in columns"
        :key="column.key"
        type="button"
        role="menuitemcheckbox"
        :aria-checked="visible.includes(column)"
        :aria-disabled="isAlwaysVisible(column.key)"
        class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-700"
        @click="toggleColumn(column.key)"
      >
        <Icon :name="visible.includes(column) ? 'check' : 'x'" size="sm" />
        {{ column.label }}
      </button>
      <button
        type="button"
        class="w-full border-t border-gray-100 px-3 py-2 text-left text-sm text-primary-600 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
        @click="emit('reset')"
      >
        {{ $t('vendor.restoreColumns') }}
      </button>
    </div>
  </Teleport>
</template>
