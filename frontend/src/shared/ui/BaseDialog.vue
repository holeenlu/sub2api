<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" tabindex="-1" :class="['modal-content', widthClasses, contentClass]" @click.stop>
          <!-- Header -->
          <div class="modal-header">
            <slot name="header" :title-id="dialogId">
              <h3 :id="dialogId" class="modal-title">
                {{ title }}
              </h3>
              <button
                v-if="showCloseButton"
                @click="emit('close')"
                class="-mr-2 rounded-xl p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900"
                :aria-label="t('ui.closeModal')"
              >
                <Icon name="x" size="md" />
              </button>
            </slot>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" :class="['modal-body', bodyClass]">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let dialogIdCounter = 0
const openDialogs = new Set<string>()
</script>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useBodyScrollLock } from '@/shared/lib/useBodyScrollLock'

const { t } = useI18n()

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null
let disposed = false

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
  contentClass?: string
  bodyClass?: string
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  zIndex: 50
})

const emit = defineEmits<Emits>()

useBodyScrollLock(() => props.show)

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const isTopDialog = (): boolean => [...openDialogs].pop() === dialogId
const canReceiveFocus = (element: HTMLElement): boolean => {
  if (element.matches(':disabled') || element.closest('[hidden], [inert], [aria-hidden="true"]')) return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    const style = getComputedStyle(node)
    if (style.display === 'none' || style.visibility === 'hidden') return false
  }
  return true
}

const focusableElements = (): HTMLElement[] => {
  const root = dialogRef.value
  if (!root) return []
  const candidates = Array.from(root.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex], [contenteditable="true"]'
  )).filter(element => element.tabIndex >= 0 && canReceiveFocus(element))
  // A native radio group is one Tab stop: the checked item (or its first
  // enabled item). Including unchecked siblings lets reverse Tab escape.
  return candidates.filter(element => {
    if (!(element instanceof HTMLInputElement) || element.type !== 'radio' || !element.name) return true
    const group = candidates.filter((candidate): candidate is HTMLInputElement =>
      candidate instanceof HTMLInputElement && candidate.type === 'radio'
      && candidate.name === element.name && candidate.form === element.form)
    return (group.find(radio => radio.checked) ?? group[0]) === element
  })
    .sort((left, right) => (left.tabIndex || Infinity) - (right.tabIndex || Infinity))
}

const handleKeydown = (event: KeyboardEvent): void => {
  if (!props.show || disposed || !isTopDialog() || event.defaultPrevented) return
  if (props.closeOnEscape && event.key === 'Escape') {
    emit('close')
  } else if (event.key === 'Tab') {
    const elements = focusableElements()
    const first = elements[0]
    const last = elements[elements.length - 1]
    const active = document.activeElement
    if (!first || !elements.includes(active as HTMLElement)) {
      event.preventDefault()
      ;(event.shiftKey ? last : first)?.focus()
      if (!first) dialogRef.value?.focus()
    } else if (event.shiftKey && active === first) {
      event.preventDefault()
      last?.focus()
    } else if (!event.shiftKey && active === last) {
      event.preventDefault()
      first.focus()
    }
  }
}

const releaseDialog = (): void => {
  const restore = isTopDialog()
  openDialogs.delete(dialogId)
  if (restore) {
    const nextId = [...openDialogs].pop()
    const nextDialog = nextId
      ? document.querySelector<HTMLElement>(`[aria-labelledby="${nextId}"] .modal-content`)
      : null
    if (previousActiveElement?.isConnected && canReceiveFocus(previousActiveElement) && (!nextDialog || nextDialog.contains(previousActiveElement))) {
      previousActiveElement.focus()
    } else {
      nextDialog?.focus()
    }
  }
  previousActiveElement = null
}

// Track dialog focus ownership independently of the shared body scroll lock
watch(
  () => props.show,
  async (isOpen, _wasOpen, onCleanup) => {
    let invalidated = false
    onCleanup(() => { invalidated = true })
    if (isOpen) {
      // 保存当前焦点元素
      previousActiveElement = document.activeElement as HTMLElement
      openDialogs.add(dialogId)

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
      if (invalidated || disposed || !props.show || !isTopDialog()) return
      if (modalBodyRef.value) {
        modalBodyRef.value.scrollTop = 0
      }
      if (dialogRef.value) {
        const target = focusableElements()[0] ?? dialogRef.value
        target.focus()
      }
    } else {
      releaseDialog()
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  disposed = true
  document.removeEventListener('keydown', handleKeydown)
  // 释放焦点所有权；滚动锁由组件scope独立释放
  releaseDialog()
})
</script>
