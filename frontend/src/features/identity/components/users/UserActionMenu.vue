<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useAuthStore } from '../../model/auth'
import type { AdminUser } from '../../api/profileTypes'

defineProps<{ users: AdminUser[]; activeMenuId: number | null; menuPosition: { top: number; left: number } | null }>()
const emit = defineEmits<{
  apiKeys: [user: AdminUser]; groups: [user: AdminUser]; deposit: [user: AdminUser]
  withdraw: [user: AdminUser]; platformQuota: [user: AdminUser]; history: [user: AdminUser]; delete: [user: AdminUser]; close: []
}>()
const { t } = useI18n()
const rolePermissions = useAuthStore()
</script>

<template>
    <Teleport to="body">
      <div
        v-if="activeMenuId !== null && menuPosition"
        class="action-menu-content fixed z-[9999] w-48 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10"
        :style="{ top: menuPosition.top + 'px', left: menuPosition.left + 'px' }"
      >
        <div class="py-1">
          <template v-for="user in users" :key="user.id">
            <template v-if="user.id === activeMenuId">
              <!-- View API Keys -->
              <button v-if="rolePermissions.can('api_keys.read')"
                @click="emit('apiKeys', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <Icon name="key" size="sm" class="text-gray-400" :stroke-width="2" />
                {{ t('admin.users.apiKeys') }}
              </button>

              <!-- Allowed Groups -->
              <button v-if="(rolePermissions.isSuperAdmin || (user.role === 'user' && rolePermissions.can('users.update')))"
                @click="emit('groups', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <Icon name="users" size="sm" class="text-gray-400" :stroke-width="2" />
                {{ t('admin.users.groups') }}
              </button>

              <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>

              <!-- Deposit -->
              <button v-if="rolePermissions.can('billing.balance.adjust') &amp;&amp; user.id !== rolePermissions.user?.id &amp;&amp; user.role !== 'super_admin'"
                @click="emit('deposit', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <Icon name="plus" size="sm" class="text-emerald-500" :stroke-width="2" />
                {{ t('admin.users.deposit') }}
              </button>

              <!-- Withdraw -->
              <button v-if="rolePermissions.can('billing.balance.adjust') &amp;&amp; user.id !== rolePermissions.user?.id &amp;&amp; user.role !== 'super_admin'"
                @click="emit('withdraw', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <svg class="h-4 w-4 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 12H4" />
                </svg>
                {{ t('admin.users.withdraw') }}
              </button>

              <!-- Platform Quotas -->
              <button v-if="(rolePermissions.isSuperAdmin || (user.role === 'user' && rolePermissions.can('users.update')))"
                @click="emit('platformQuota', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <Icon name="chartBar" size="sm" class="text-gray-400" :stroke-width="2" />
                {{ t('admin.users.platformQuota.menuItem') }}
              </button>

              <!-- Balance History -->
              <button
                @click="emit('history', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <Icon name="dollar" size="sm" class="text-gray-400" :stroke-width="2" />
                {{ t('admin.users.balanceHistory') }}
              </button>

              <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>

              <!-- Delete an eligible member. -->
              <button
                v-if="(rolePermissions.isSuperAdmin || (user.role === 'user' && rolePermissions.can('users.delete'))) &amp;&amp; user.id !== rolePermissions.user?.id &amp;&amp; (rolePermissions.isSuperAdmin || user.role !== 'super_admin')"
                @click="emit('delete', user); emit('close')"
                class="flex w-full items-center gap-2 px-4 py-2 text-sm text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
              >
                <Icon name="trash" size="sm" :stroke-width="2" />
                {{ t('common.delete') }}
              </button>
            </template>
          </template>
        </div>
      </div>
    </Teleport>
</template>
