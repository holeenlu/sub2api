<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useAuthStore } from '../../model/auth'
import type { AdminUser } from '../../api/profileTypes'

defineProps<{ user: AdminUser; activeMenuId: number | null }>()
const emit = defineEmits<{ edit: [user: AdminUser]; toggleStatus: [user: AdminUser]; menu: [user: AdminUser, event: MouseEvent] }>()
const { t } = useI18n()
const rolePermissions = useAuthStore()
</script>

<template>
            <div class="flex items-center gap-1">
              <!-- Edit Button -->
              <button v-if="(rolePermissions.isSuperAdmin || (user.role === 'user' && rolePermissions.can('users.update')))"
                @click="emit('edit', user)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t('common.edit') }}</span>
              </button>

              <!-- Peer administrators are manageable; self and protected roots are not. -->
              <button
                v-if="(rolePermissions.isSuperAdmin || (user.role === 'user' && rolePermissions.can('users.update'))) &amp;&amp; user.id !== rolePermissions.user?.id &amp;&amp; (rolePermissions.isSuperAdmin || user.role !== 'super_admin')"
                @click="emit('toggleStatus', user)"
                :class="[
                  'flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors',
                  user.status === 'active'
                    ? 'hover:bg-orange-50 hover:text-orange-600 dark:hover:bg-orange-900/20 dark:hover:text-orange-400'
                    : 'hover:bg-green-50 hover:text-green-600 dark:hover:bg-green-900/20 dark:hover:text-green-400'
                ]"
              >
                <Icon v-if="user.status === 'active'" name="ban" size="sm" />
                <Icon v-else name="checkCircle" size="sm" />
                <span class="text-xs">{{ user.status === 'active' ? t('admin.users.disable') : t('admin.users.enable') }}</span>
              </button>

              <!-- More Actions Menu Trigger -->
              <button
                @click="emit('menu', user, $event)"
                class="action-menu-trigger flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-dark-700 dark:hover:text-white"
                :class="{ 'bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-white': activeMenuId === user.id }"
              >
                <Icon name="more" size="sm" />
                <span class="text-xs">{{ t('common.more') }}</span>
              </button>
            </div>
</template>
