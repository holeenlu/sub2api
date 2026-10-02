<template>
  <section class="space-y-3">
    <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t(`docs.models.${kind}`) }} · {{ t('docs.livePrice') }}</h2>
    <PlazaGroupSection v-if="displayGroup" :group="displayGroup" />
    <p v-else class="text-sm text-gray-500 dark:text-dark-400">{{ t('modelPlaza.empty') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlazaGroupSection from '@/components/modelPlaza/PlazaGroupSection.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import { docsModelCatalogById } from '@/content/docs/modelCatalog'

const props = defineProps<{ kind: 'chat' | 'image'; selectedGroup?: ModelPlazaGroup | null; modelIds?: string[] }>()
const { t } = useI18n()
const displayGroup = computed(() => {
  if (!props.selectedGroup) return null
  return { ...props.selectedGroup, models: props.selectedGroup.models.filter(model => {
    if (props.modelIds) return props.modelIds.includes(model.name)
    const kind = docsModelCatalogById.get(model.name)?.kind ?? (model.pricing?.billing_mode === 'image' ? 'image' : 'chat')
    return kind === props.kind
  }) }
})
</script>
