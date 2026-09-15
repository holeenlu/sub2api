<template>
  <nav class="space-y-7">
    <DocsGroupSelect :groups="groups" :model-value="selectedGroupId" :show-label="true" :loading="loading" @update:model-value="$emit('update:selectedGroupId', $event)" />
    <section v-for="group in docsNavGroups" :key="group.titleKey">
      <h2 class="mb-2 px-2 text-[11px] font-semibold uppercase text-gray-400">{{ t(group.titleKey) }}</h2>
      <div class="space-y-0.5">
        <RouterLink v-for="item in group.items" :key="item.path" :to="item.path" class="flex min-h-9 items-center gap-2 rounded-md px-2 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white" :class="route.path === item.path ? 'bg-primary-50 font-medium text-primary-800 dark:bg-primary-950/40 dark:text-primary-300' : ''" @click="$emit('navigate')">
          <Icon :name="item.icon" size="xs" class="shrink-0" />{{ t(item.titleKey) }}
        </RouterLink>
      </div>
    </section>
    <section v-if="selectedGroup">
      <h2 class="mb-2 px-2 text-[11px] font-semibold uppercase text-gray-400">{{ t('docs.nav.availableModels') }}</h2>
      <div class="space-y-0.5">
        <RouterLink v-for="model in selectedGroup.models" :key="model.name" :to="`/docs/models/${model.name}`" class="block truncate rounded-md px-2 py-1.5 font-mono text-xs text-gray-500 hover:bg-gray-50 hover:text-primary-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-primary-400" @click="$emit('navigate')">{{ model.name }}</RouterLink>
      </div>
    </section>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DocsGroupSelect from './DocsGroupSelect.vue'
import { docsNavGroups } from '@/content/docs/nav'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

const props = defineProps<{ groups: ModelPlazaGroup[]; selectedGroupId: number | null; loading?: boolean }>()
defineEmits<{ 'update:selectedGroupId': [value: number]; navigate: [] }>()
const route = useRoute()
const { t } = useI18n()
const selectedGroup = computed(() => props.groups.find((group) => group.id === props.selectedGroupId) ?? props.groups[0])
</script>
