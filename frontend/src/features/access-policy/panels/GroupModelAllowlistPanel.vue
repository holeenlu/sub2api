<template>
        <div class="border-t pt-4">
          <div class="mb-3 flex items-center justify-between gap-3">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t("admin.groups.modelAllowlist.title") }}
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelAllowlist.hint") }}
              </p>
            </div>
            <Toggle v-model="editModelAllowlistState.enabled" />
          </div>
          <div
            v-if="editModelAllowlistState.enabled"
            class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50/50 dark:border-dark-600 dark:bg-dark-800/40"
          >
            <div
              v-if="!editModelAllowlistLoading && editModelAllowlistState.items.length > 0"
              class="flex items-center justify-between gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-600 dark:bg-dark-800"
            >
              <span class="text-gray-500 dark:text-gray-400">
                {{
                  t("admin.groups.modelAllowlist.selectedSummary", {
                    selected: editModelAllowlistSelectedCount,
                    total: editModelAllowlistState.items.length,
                  })
                }}
              </span>
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  class="rounded px-2 py-1 font-medium text-primary-600 transition-colors hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                  @click="selectAllModelAllowlistItems(editModelAllowlistState)"
                >
                  {{ t("admin.groups.modelAllowlist.selectAll") }}
                </button>
                <button
                  type="button"
                  class="rounded px-2 py-1 font-medium text-gray-600 transition-colors hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                  @click="invertModelAllowlistSelection(editModelAllowlistState)"
                >
                  {{ t("admin.groups.modelAllowlist.invertSelection") }}
                </button>
              </div>
            </div>
            <div
              class="max-h-64 space-y-2 overflow-y-auto p-2"
            >
              <p v-if="editModelAllowlistLoading" class="text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelAllowlist.loading") }}
              </p>
              <p
                v-else-if="editModelAllowlistState.items.length === 0"
                class="text-xs text-gray-500 dark:text-gray-400"
              >
                {{ t("admin.groups.modelAllowlist.empty") }}
              </p>
              <div
                v-for="(item, index) in editModelAllowlistState.items"
                :key="item.id"
                class="flex items-center gap-2 rounded border border-gray-200 bg-white px-3 py-2 dark:border-dark-600 dark:bg-dark-800"
              >
                <input
                  v-model="item.selected"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <span class="min-w-0 flex-1 break-all text-sm text-gray-700 dark:text-gray-300">
                  {{ item.id }}
                  <span
                    v-if="item.id.includes('*')"
                    class="ml-1 rounded bg-primary-50 px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:bg-primary-900/30 dark:text-primary-400"
                  >
                    {{ t("admin.groups.modelAllowlist.wildcardTag") }}
                  </span>
                </span>
                <button
                  type="button"
                  :disabled="index === 0"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveEditModelAllowlistItem(index, index - 1)"
                >
                  <Icon name="arrowUp" size="sm" />
                </button>
                <button
                  type="button"
                  :disabled="index === editModelAllowlistState.items.length - 1"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveEditModelAllowlistItem(index, index + 1)"
                >
                  <Icon name="arrowDown" size="sm" />
                </button>
              </div>
            </div>
            <div class="border-t border-gray-200 px-3 py-2 dark:border-dark-600">
              <div class="flex items-center gap-2">
                <input
                  v-model="editAllowlistCustomEntry"
                  type="text"
                  :placeholder="t('admin.groups.modelAllowlist.customPlaceholder')"
                  class="min-w-0 flex-1 rounded border border-gray-300 bg-white px-2 py-1.5 text-sm text-gray-700 focus:border-primary-500 focus:outline-none dark:border-dark-500 dark:bg-dark-700 dark:text-gray-200"
                  @keydown.enter.prevent="submitEditAllowlistCustomEntry"
                />
                <button
                  type="button"
                  class="rounded bg-primary-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-primary-700"
                  @click="submitEditAllowlistCustomEntry"
                >
                  {{ t("admin.groups.modelAllowlist.addCustom") }}
                </button>
              </div>
              <p
                v-if="editAllowlistCustomErrorKey"
                class="mt-1 text-xs text-red-500"
              >
                {{ t(editAllowlistCustomErrorKey) }}
              </p>
            </div>
          </div>
        </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import type { GroupPlatform, ModelAllowlist } from '@/features/access-policy/api/groupTypes';
import Toggle from '@/shared/ui/Toggle.vue';
import Icon from '@/shared/ui/icons/Icon.vue';
import { groupsAPI } from '../api/groups';
import { useAccessPolicyRequest } from '../queries/requestScope';
import { useDraftSection } from '@/shared/lib/useDraftSection';
import { createModelAllowlistState, setModelAllowlistCandidates, addCustomModelAllowlistItem, selectAllModelAllowlistItems, invertModelAllowlistSelection, moveModelAllowlistItem, buildModelAllowlistConfig } from '../model/groupModelAllowlist';
import { accessPolicyNotificationsKey, requireAccessPolicyService } from '../services';
const props = defineProps<{ modelValue: ReturnType<typeof createModelAllowlistState>; platform: GroupPlatform; groupId: number }>()
const emit = defineEmits<{ 'update:modelValue': [value: ReturnType<typeof createModelAllowlistState>] }>()
const editModelAllowlistState = useDraftSection(() => props.modelValue, patch => emit('update:modelValue', { ...props.modelValue, ...patch }))
const { t } = useI18n()
const notifications = requireAccessPolicyService(accessPolicyNotificationsKey)
const editModelAllowlistLoading = ref(false)
const editModelAllowlistSelectedCount = computed(() => editModelAllowlistState.items.filter(item => item.selected).length)
const editAllowlistCustomEntry = ref('')
const editAllowlistCustomErrorKey = ref<string | null>(null)
const pending = useAccessPolicyRequest(() => { editModelAllowlistLoading.value = false })
async function load() {
 const request = pending.begin(); editModelAllowlistLoading.value = true
 try {
  const models = await groupsAPI.getModelAllowlistCandidates(props.groupId, props.platform, { signal: request.signal })
  if (request.isCurrent()) setModelAllowlistCandidates(editModelAllowlistState, models)
 } catch { if (request.isCurrent()) notifications.showError(t('admin.groups.failedToLoad')) }
 finally { if (request.isCurrent()) editModelAllowlistLoading.value = false }
}
watch([() => props.platform, () => props.groupId, pending.scope], load, { immediate: true })
const submitEditAllowlistCustomEntry = () => {
  const error = addCustomModelAllowlistItem(
    editModelAllowlistState,
    editAllowlistCustomEntry.value,
  );
  if (error === null) {
    editAllowlistCustomEntry.value = "";
    editAllowlistCustomErrorKey.value = null;
  } else {
    editAllowlistCustomErrorKey.value = `admin.groups.modelAllowlist.errors.${error}`;
  }
};
const moveEditModelAllowlistItem = (fromIndex: number, toIndex: number) => {
  moveModelAllowlistItem(editModelAllowlistState, fromIndex, toIndex);
};
defineExpose({ validate: () => true, payload: (): ModelAllowlist => buildModelAllowlistConfig(editModelAllowlistState) })

</script>
