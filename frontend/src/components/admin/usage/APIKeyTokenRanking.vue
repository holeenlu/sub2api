<template>
  <!-- 用量页"API 密钥排行"tab 内容：公共排行骨架 + 密钥/所属用户两列身份列 -->
  <BreakdownRanking
    ref="rankingRef"
    :fetch="fetchKeys"
    :row-key="(item) => item.api_key_id"
    :identity-columns="identityColumns"
    subtitle="admin.usage.keyRanking.subtitle"
    count-label="admin.usage.keyRanking.keyCount"
    row-hint="admin.usage.keyRanking.rowHint"
    :start-date="startDate"
    :end-date="endDate"
    :filters="filters"
    :model="model"
    :visible-column-keys="visibleColumnKeys"
    :active="active"
    @select="(item) => $emit('select-api-key', item.api_key_id, keyLabel(item))"
  >
    <template #cell-key="{ item }">
      <span :title="keyLabel(item)">{{ keyLabel(item) }}</span>
      <!-- 名字为空时 keyLabel 已含 #id，不再重复追加 -->
      <span v-if="item.key_name" class="ml-1 font-normal text-gray-400 dark:text-gray-500">#{{ item.api_key_id }}</span>
      <span
        v-if="item.key_deleted"
        data-testid="api-key-ranking-deleted"
        class="ml-1.5 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-normal text-gray-500 dark:bg-dark-700 dark:text-gray-400"
      >{{ t('usage.errors.keyDeleted') }}</span>
    </template>
    <template #cell-user="{ item }">
      <span :title="item.email">{{ item.email || `User #${item.user_id}` }}</span>
    </template>
  </BreakdownRanking>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAPIKeyBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import type { APIKeyBreakdownItem } from '@/types'
import BreakdownRanking, { type BreakdownIdentityColumn } from './BreakdownRanking.vue'

withDefaults(defineProps<{
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
  /** 由父级列设置下拉控制；未传时全部显示。key 集合见 UsageView 的 keyRankingAllColumns */
  visibleColumnKeys?: string[]
  active?: boolean
}>(), {
  active: true,
})

defineEmits<{ (e: 'select-api-key', apiKeyId: number, keyName: string): void }>()

const { t } = useI18n()

const identityColumns: BreakdownIdentityColumn[] = [
  { key: 'key', label: 'admin.usage.keyRanking.columns.key' },
  { key: 'user', label: 'admin.usage.keyRanking.columns.user', cellClass: 'max-w-[220px] text-gray-500 dark:text-gray-400' },
]

const fetchKeys = async (params: UserBreakdownParams): Promise<APIKeyBreakdownItem[]> => {
  const res = await getAPIKeyBreakdown(params)
  return res.api_keys || []
}

// Key 已被物理删除时 key_name 为空，退回显示 ID，避免整列空白。
const keyLabel = (item: APIKeyBreakdownItem) => item.key_name || `Key #${item.api_key_id}`

// 泛型组件拿不到 InstanceType，只声明用到的 expose 形状
const rankingRef = ref<{ reload: () => Promise<void> } | null>(null)
defineExpose({ reload: () => rankingRef.value?.reload() })
</script>
