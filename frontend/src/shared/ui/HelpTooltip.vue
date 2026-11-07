<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useId, useTemplateRef, nextTick } from 'vue'

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click' | 'both'
  widthClass?: string
  tone?: 'default' | 'brand' | 'surface'
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
  tone: 'default',
})

const show = ref(false)
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const arrowLeft = ref('50%')
const tooltipId = useId()
const pinned = ref(false)
let hideTimer: ReturnType<typeof setTimeout> | undefined
let selecting = false
let pointerInside = false
let selectionClickPending = false

function cancelHide() {
  clearTimeout(hideTimer)
  hideTimer = undefined
}

function hasPanelSelection() {
  const selection = window.getSelection()
  return !!selection?.toString() &&
    (isInside(tooltipRef.value, selection.anchorNode) || isInside(tooltipRef.value, selection.focusNode))
}

function hasFocus() {
  return isInside(triggerRef.value, document.activeElement) || isInside(tooltipRef.value, document.activeElement)
}

function deferClose() {
  cancelHide()
  if (!show.value || props.trigger === 'click' || pointerInside || hasFocus()) return
  if (props.trigger !== 'both') {
    closeTooltip()
    return
  }
  if (pinned.value || selecting || hasPanelSelection()) return
  hideTimer = setTimeout(closeTooltip, 250)
}

function onPanelEnter() {
  pointerInside = true
  cancelHide()
}

function onPanelPointerDown(event: PointerEvent) {
  if (props.trigger !== 'both' || event.button !== 0) return
  cancelHide()
  selecting = true
}

function onPointerCancel() {
  if (!selecting) return
  selecting = false
  selectionClickPending = false
  deferClose()
}

function onPointerUp(event: PointerEvent) {
  if (!selecting) return
  selecting = false
  selectionClickPending = hasPanelSelection()
  if (isInside(tooltipRef.value, event.target) || isInside(triggerRef.value, event.target)) return
  deferClose()
}

function onDocumentKeyup() {
  if (props.trigger === 'both') deferClose()
}

function onDocumentPointerDown() {
  // A new gesture must be able to dismiss a previously selected panel.
  selectionClickPending = false
}

function openTooltip() {
  cancelHide()
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  cancelHide()
  pinned.value = false
  selecting = false
  pointerInside = false
  selectionClickPending = false
  show.value = false
}

function onEnter() {
  pointerInside = true
  if (props.trigger === 'click') return
  openTooltip()
}

function onFocusIn() {
  cancelHide()
  if (props.trigger !== 'click') openTooltip()
}

function onFocusOut(event: FocusEvent) {
  if (isInside(triggerRef.value, event.relatedTarget) || isInside(tooltipRef.value, event.relatedTarget)) return
  // Wait for activeElement to reflect the destination of the focus move.
  nextTick(deferClose)
}

function isInside(container: HTMLElement | null, target: EventTarget | null): boolean {
  return target instanceof Node && !!container?.contains(target)
}

// 悬停模式下指针在触发图标与提示框之间往返时保持打开，便于选中提示里的文字。
function onLeave(event: MouseEvent) {
  pointerInside = isInside(tooltipRef.value, event.relatedTarget) || isInside(triggerRef.value, event.relatedTarget)
  if (props.trigger === 'click') return
  if (pointerInside) return
  deferClose()
}

function onTooltipLeave(event: MouseEvent) {
  onLeave(event)
}

function onClick(event: MouseEvent) {
  if (props.trigger === 'hover') return
  event.stopPropagation()
  if (props.trigger === 'both') {
    if (pinned.value) {
      closeTooltip()
      return
    }
    pinned.value = true
    openTooltip()
    return
  }
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if (props.trigger === 'hover' || !show.value) return
  const selectionClick = selectionClickPending && hasPanelSelection()
  selectionClickPending = false
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  // A drag can release outside the panel and still produce a click there.
  if (props.trigger === 'both' && selectionClick) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const el = triggerRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const center = rect.left + rect.width / 2
  const width = tooltipRef.value?.getBoundingClientRect().width || 0
  const left = props.trigger === 'both' || props.tone !== 'default'
    ? Math.max(width / 2 + 8, Math.min(center, window.innerWidth - width / 2 - 8))
    : center
  const panelHeight = tooltipRef.value?.getBoundingClientRect().height || 0
  tooltipStyle.value = {
    // Teleported panels are fixed: DOMRect already contains viewport coordinates.
    top: `${props.trigger === 'both' ? Math.max(8, Math.min(rect.top - panelHeight - 8 >= 8 ? rect.top - panelHeight - 8 : rect.bottom + 8, window.innerHeight - panelHeight - 8)) : rect.top}px`,
    left: `${left}px`,
  }
  arrowLeft.value = props.tone === 'brand' ? `${Math.max(8, Math.min(width - 8, center - left + width / 2))}px` : '50%'
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  document.addEventListener('pointerup', onPointerUp)
  document.addEventListener('pointercancel', onPointerCancel)
  document.addEventListener('keyup', onDocumentKeyup)
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  closeTooltip()
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  document.removeEventListener('pointerup', onPointerUp)
  document.removeEventListener('pointercancel', onPointerCancel)
  document.removeEventListener('keyup', onDocumentKeyup)
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    class="group relative ml-1 inline-flex items-center align-middle"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @click="onClick"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
  >
    <!-- Trigger Icon -->
    <slot name="trigger" :expanded="show" :tooltip-id="tooltipId">
      <button type="button" :aria-expanded="show" :aria-describedby="show ? tooltipId : undefined" :aria-label="content || '?'" class="inline-flex rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500">
      <svg
        aria-hidden="true"
        class="h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
      </button>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <!-- before: 伪元素向下延伸一段透明区域，盖住提示框与触发图标之间的空隙，让指针能连续移入提示框。 -->
      <div
        ref="tooltip"
        :id="tooltipId"
        v-show="show"
        role="tooltip"
        :class="[
          'fixed z-[99999] -translate-x-1/2 rounded-lg text-xs shadow-xl selection:bg-primary-200 selection:text-gray-900 before:absolute before:inset-x-0 before:top-full before:h-3 dark:selection:bg-primary-200 dark:selection:text-gray-900',
          props.trigger === 'both' ? 'max-h-[calc(100dvh-16px)] overflow-auto' : '-translate-y-full',
          props.tone === 'surface'
            ? 'border border-gray-200 bg-white p-3 leading-[18px] text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200'
            : props.tone === 'brand'
            ? 'bg-primary-400 px-3 py-2 leading-[18px] text-primary-950'
            : 'bg-gray-900 p-3 leading-relaxed text-white ring-1 ring-white/10 dark:bg-gray-800',
          props.widthClass,
        ]"
        :style="{ top: props.trigger === 'both' ? tooltipStyle.top : `calc(${tooltipStyle.top} - 8px)`, left: tooltipStyle.left }"
        @mouseenter="onPanelEnter"
        @mouseleave="onTooltipLeave"
        @pointerdown="onPanelPointerDown"
        @focusin="cancelHide"
        @focusout="onFocusOut"
      >
        <button
          v-if="props.trigger === 'click'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded p-1 text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
          aria-label="Close"
          @click.stop="closeTooltip"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
        <slot>{{ content }}</slot>
        <div v-if="props.trigger !== 'both'" class="absolute -bottom-1 h-2 w-2 -translate-x-1/2 rotate-45" :style="{ left: arrowLeft }" :class="props.tone === 'surface' ? 'bg-white dark:bg-dark-800' : props.tone === 'brand' ? 'bg-primary-400' : 'bg-gray-900 dark:bg-gray-800'"></div>
      </div>
    </Teleport>
  </div>
</template>
