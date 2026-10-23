<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, useId } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'
import { getFloatingPanelPosition } from '@/utils/floatingPanel'

defineProps<{ label: string; compact?: boolean; disabled?: boolean }>()
const open = ref(false)
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLDivElement>()
const position = ref<ReturnType<typeof getFloatingPanelPosition>>()
const id = useId()
const style = computed(() =>
  position.value
    ? {
        top: position.value.top == null ? 'auto' : position.value.top + 'px',
        bottom: position.value.bottom == null ? 'auto' : position.value.bottom + 'px',
        left: position.value.left + 'px',
        width: position.value.width + 'px',
        maxHeight: position.value.maxHeight + 'px'
      }
    : {}
)
function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}
async function toggle() {
  if (open.value) return close()
  if (!trigger.value) return
  position.value = getFloatingPanelPosition(
    trigger.value.getBoundingClientRect(),
    window.innerWidth,
    window.innerHeight,
    { maxWidth: 208 }
  )
  open.value = true
  await nextTick()
  panel.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
}
function navigate(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.stopPropagation()
    close(true)
    return
  }
  if (event.key === 'Tab') {
    close(true)
    return
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const buttons = Array.from(
    panel.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || []
  )
  if (!buttons.length) return
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
  buttons[
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? buttons.length - 1
        : (index + (event.key === 'ArrowUp' ? -1 : 1) + buttons.length) % buttons.length
  ]?.focus()
}
onClickOutside(panel, () => close(), { ignore: [trigger] })
useEventListener(window, 'resize', () => close())
useEventListener(
  window,
  'scroll',
  (event) => {
    if (event.target instanceof Node && !panel.value?.contains(event.target)) close()
  },
  { capture: true }
)
onUnmounted(() => close())
</script>

<template>
  <button
    ref="trigger"
    type="button"
    :class="
      compact
        ? 'rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700'
        : 'btn btn-secondary btn-sm'
    "
    :disabled="disabled"
    :aria-label="label"
    :title="label"
    :aria-expanded="open"
    :aria-controls="id"
    aria-haspopup="menu"
    @click="toggle"
    @keydown.down.prevent="toggle"
  >
    <Icon name="more" size="sm" /><span :class="{ 'sr-only': compact }">{{ label }}</span
    ><Icon v-if="!compact" name="chevronDown" size="xs" />
  </button>
  <Teleport to="body"
    ><div
      v-if="open"
      :id="id"
      ref="panel"
      role="menu"
      :aria-label="label"
      :style="style"
      class="vendor-action-menu fixed z-[100] overflow-y-auto rounded-xl border border-gray-200 bg-white p-1.5 shadow-xl dark:border-dark-600 dark:bg-dark-800"
      @keydown="navigate"
      @click="close(true)"
    >
      <slot /></div
  ></Teleport>
</template>

<style scoped>
.vendor-action-menu :deep(button) {
  @apply flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-gray-100 focus-visible:bg-gray-100 focus-visible:outline-none disabled:opacity-40 dark:hover:bg-dark-700 dark:focus-visible:bg-dark-700;
}
</style>
