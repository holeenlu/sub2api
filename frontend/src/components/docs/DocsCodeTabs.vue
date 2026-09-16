<template>
  <section class="overflow-hidden rounded-lg border border-gray-200 bg-gray-950 dark:border-dark-700">
    <div class="flex min-h-11 items-center justify-between border-b border-white/10 px-2">
      <div class="flex items-center" role="tablist" :aria-label="t('docs.requestExample')">
        <button
          v-for="example in examples"
          :key="example.id"
          type="button"
          role="tab"
          :aria-selected="activeId === example.id"
          class="h-11 px-3 text-xs font-medium transition-colors"
          :class="activeId === example.id ? 'border-b-2 border-primary-400 text-white' : 'text-gray-400 hover:text-gray-200'"
          @click="activeId = example.id"
        >
          {{ example.label }}
        </button>
      </div>
      <button
        type="button"
        class="flex h-8 w-8 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-white/10 hover:text-white"
        :title="copied ? t('docs.copied') : t('docs.copy')"
        @click="copyCode"
      >
        <Icon :name="copied ? 'check' : 'copy'" size="sm" />
      </button>
    </div>
    <pre class="min-h-48 overflow-x-auto p-4 text-[13px] leading-6 text-gray-200"><code>{{ activeExample?.code }}</code></pre>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { DocsCodeExample } from '@/content/docs/types'

const props = defineProps<{ examples: DocsCodeExample[] }>()
const { t } = useI18n()
const activeId = ref<DocsCodeExample['id']>(props.examples[0]?.id ?? 'curl')
const copied = ref(false)
const activeExample = computed(() => props.examples.find((item) => item.id === activeId.value) ?? props.examples[0])

watch(() => props.examples, (items) => {
  if (!items.some((item) => item.id === activeId.value)) activeId.value = items[0]?.id ?? 'curl'
})

async function copyCode() {
  if (!activeExample.value) return
  await navigator.clipboard.writeText(activeExample.value.code)
  copied.value = true
  window.setTimeout(() => { copied.value = false }, 1600)
}
</script>
