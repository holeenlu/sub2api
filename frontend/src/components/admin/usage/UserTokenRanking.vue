<template>
  <!-- 用量页"用户排行"tab 内容：公共排行骨架 + 用户身份列 -->
  <BreakdownRanking
    ref="rankingRef"
    :fetch="fetchUsers"
    :row-key="(item) => item.user_id"
    :identity-columns="identityColumns"
    subtitle="admin.usage.tokenRanking.subtitle"
    count-label="admin.usage.tokenRanking.userCount"
    row-hint="admin.usage.tokenRanking.rowHint"
    :start-date="startDate"
    :end-date="endDate"
    :filters="filters"
    :model="model"
    :visible-column-keys="visibleColumnKeys"
    :active="active"
    @select="(item) => $emit('select-user', item.user_id, item.email)"
  >
    <template #cell-user="{ item }">
      <span :title="item.email">{{ item.email || `User #${item.user_id}` }}</span>
      <span class="ml-1 font-normal text-gray-400 dark:text-gray-500">#{{ item.user_id }}</span>
    </template>
  </BreakdownRanking>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import type { UserBreakdownItem } from '@/types'
import BreakdownRanking, { type BreakdownIdentityColumn } from './BreakdownRanking.vue'

withDefaults(defineProps<{
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
  visibleColumnKeys?: string[]
  active?: boolean
}>(), {
  active: true,
})

defineEmits<{ (e: 'select-user', userId: number, email: string): void }>()

const identityColumns: BreakdownIdentityColumn[] = [
  { key: 'user', label: 'admin.usage.tokenRanking.columns.user' },
]

const fetchUsers = async (params: UserBreakdownParams): Promise<UserBreakdownItem[]> => {
  const res = await getUserBreakdown(params)
  return res.users || []
}

// 泛型组件拿不到 InstanceType，只声明用到的 expose 形状
const rankingRef = ref<{ reload: () => Promise<void> } | null>(null)
defineExpose({ reload: () => rankingRef.value?.reload() })
</script>
