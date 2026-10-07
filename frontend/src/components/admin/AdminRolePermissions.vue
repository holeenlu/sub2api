<template>
  <section v-if="auth.isSuperAdmin" class="card space-y-5 p-6" data-testid="admin-role-permissions">
    <div>
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.rolePermissions.title') }}</h2>
      <p class="mt-2 text-sm text-gray-500">{{ t('admin.rolePermissions.description') }}</p>
    </div>
    <div v-if="loading" class="py-6 text-sm">{{ t('common.loading') }}</div>
    <template v-else-if="policy">
      <p v-if="policy.version === 0" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">{{ t('admin.rolePermissions.recovery') }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <button type="button" class="btn btn-secondary" @click="selectAll">{{ t('admin.rolePermissions.selectAll') }}</button>
        <button type="button" class="btn btn-secondary" @click="selected = []">{{ t('admin.rolePermissions.clear') }}</button>
        <button type="button" class="btn btn-secondary" @click="load">{{ t('common.refresh') }}</button>
        <span class="text-xs text-gray-500">{{ t('admin.rolePermissions.version', { version: policy.version }) }}</span>
      </div>
      <p v-if="!stepUpEnabled" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">{{ t('admin.rolePermissions.stepUpDisabled') }}</p>
      <section v-for="category in categories" :key="category.key" class="space-y-3">
        <h3 class="font-medium">{{ t(`admin.rolePermissions.categories.${category.key}`) }}</h3>
        <div class="grid gap-4 md:grid-cols-2">
          <fieldset v-for="group in category.modules" :key="group.module" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <legend class="px-2 text-sm font-medium">{{ t(`admin.rolePermissions.modules.${group.module.replace(/\./g, '_')}`) }}</legend>
            <select v-if="basicPermissions(group.permissions).length" :data-module="group.module" :aria-label="t(`admin.rolePermissions.modules.${group.module.replace(/\./g, '_')}`)" :value="moduleLevel(group.permissions)" :disabled="saving" class="input mb-2" @change="setModuleLevel(group.permissions, ($event.target as HTMLSelectElement).value)">
              <option value="off">{{ t('admin.rolePermissions.levels.off') }}</option>
              <option value="read">{{ t('admin.rolePermissions.levels.read') }}</option>
              <option v-if="basicPermissions(group.permissions).some(p => !p.key.endsWith('.read'))" value="manage">{{ t('admin.rolePermissions.levels.manage') }}</option>
            </select>
            <label v-for="permission in group.permissions.filter(p => !basicPermissions(group.permissions).includes(p))" :key="permission.key" class="flex cursor-pointer items-start gap-2 py-2 text-sm">
              <input type="checkbox" class="mt-0.5" :data-permission="permission.key" :checked="selected.includes(permission.key)" :disabled="saving" @change="toggle(permission, ($event.target as HTMLInputElement).checked)" />
              <span>{{ permissionLabel(permission.key) }} <span v-if="permission.sensitive" class="ml-1 text-xs text-amber-600">{{ t('admin.rolePermissions.sensitive') }}</span></span>
            </label>
          </fieldset>
        </div>
      </section>
      <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-900" data-testid="permission-preview">
        <p>{{ t('admin.rolePermissions.affected', {count: affectedAdminCount}) }}</p>
        <p class="mt-2">{{ t('admin.rolePermissions.added') }}: {{ added.map(permissionLabel).join('、') || t('admin.rolePermissions.noChanges') }}</p>
        <p class="mt-2">{{ t('admin.rolePermissions.removed') }}: {{ removed.map(permissionLabel).join('、') || t('admin.rolePermissions.noChanges') }}</p>
        <p class="mt-3 text-gray-500">{{ t('admin.rolePermissions.sessionNotice') }}</p>
      </div>
      <label class="block text-sm">
        {{ t('admin.rolePermissions.reason') }}
        <textarea v-model="reason" maxlength="512" class="input mt-2 w-full" :disabled="saving" rows="2" />
      </label>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" :disabled="saving || !dirty || !reason.trim()" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </template>
    <button v-else type="button" class="btn btn-secondary" @click="load">{{ t('common.refresh') }}</button>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { getAdminRolePermissions, updateAdminRolePermissions, type AdminPermission, type AdminRolePolicy } from '@/api/admin/roles'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const policy = ref<AdminRolePolicy | null>(null)
const catalogue = ref<AdminPermission[]>([])
const selected = ref<string[]>([])
const reason = ref('')
const affectedAdminCount = ref(0)
const stepUpEnabled = ref(true)
const categories = computed(() => ['personnel','resources','finance','operations','settings'].map(key => {
  const modules = new Map<string, AdminPermission[]>()
  for (const item of catalogue.value.filter(p => p.group === key)) modules.set(item.module, [...(modules.get(item.module) || []),item])
  return {key,modules:[...modules].map(([module, permissions]) => ({module,permissions}))}
}).filter(category => category.modules.length))
const added = computed(() => selected.value.filter(key => !policy.value?.permissions.includes(key)))
const removed = computed(() => (policy.value?.permissions || []).filter(key => !selected.value.includes(key)))
function permissionLabel(key: string) {
  const permission = catalogue.value.find(item => item.key === key)
  if (!permission) return key
  return `${t(`admin.rolePermissions.modules.${permission.module.replace(/\./g,'_')}`)} · ${t(`admin.rolePermissions.actions.${key.slice(permission.module.length+1).replace(/\./g,'_')}`)}`
}
function basicPermissions(permissions: AdminPermission[]) {
  return permissions.filter(p => !p.sensitive && ['read','manage','create','update','delete'].includes(p.key.slice(p.module.length+1)))
}
function moduleLevel(permissions: AdminPermission[]) {
  const basic = basicPermissions(permissions)
  if (basic.some(p => !p.key.endsWith('.read') && selected.value.includes(p.key))) return 'manage'
  return basic.some(p => selected.value.includes(p.key)) ? 'read' : 'off'
}
function setModuleLevel(permissions: AdminPermission[], level: string) {
  for (const p of basicPermissions(permissions).slice().reverse()) {
    if (level === 'off' || (level === 'read' && !p.key.endsWith('.read'))) toggle(p, false)
  }
  for (const p of basicPermissions(permissions)) if (level === 'manage' || (level === 'read' && p.key.endsWith('.read'))) toggle(p, true)
}
const dirty = computed(() => policy.value?.version === 0 || JSON.stringify([...selected.value].sort()) !== JSON.stringify([...(policy.value?.permissions || [])].sort()))

async function load() {
  if (!auth.isSuperAdmin) return
  loading.value = true
  try {
    const response = await getAdminRolePermissions()
    affectedAdminCount.value = response.affected_admin_count ?? affectedAdminCount.value
    stepUpEnabled.value = response.step_up_enabled ?? stepUpEnabled.value
    policy.value = response.policy
    catalogue.value = response.catalogue
    selected.value = [...response.policy.permissions]
  } catch {
    app.showError(t('admin.rolePermissions.loadFailed'))
  } finally { loading.value = false }
}
function toggle(permission: AdminPermission, checked: boolean) {
  const next = new Set(selected.value)
  const add = (key: string) => {
    if (next.has(key)) return
    next.add(key)
    for (const dependency of catalogue.value.find(item => item.key === key)?.dependencies || []) add(dependency)
  }
  if (checked) add(permission.key)
  else {
    next.delete(permission.key)
    let changed = true
    while (changed) {
      changed = false
      for (const item of catalogue.value) {
        if (next.has(item.key) && item.dependencies.some(key => !next.has(key))) {
          next.delete(item.key)
          changed = true
        }
      }
    }
  }
  selected.value = [...next].sort()
}
function selectAll() { selected.value = catalogue.value.map(item => item.key).sort() }
async function save() {
  if (!policy.value || saving.value || !reason.value.trim()) return
  const version = policy.value.version
  const permissions = [...selected.value]
  const changeReason = reason.value.trim()
  saving.value = true
  try {
    const response = await updateAdminRolePermissions(version, permissions, changeReason)
    policy.value = response.policy
    selected.value = [...response.policy.permissions]
    reason.value = ''
    app.showSuccess(t('admin.rolePermissions.saved'))
  } catch (error: any) {
    if (error?.reason === 'ADMIN_POLICY_CONFLICT') {
      await load()
      app.showError(t('admin.rolePermissions.conflict'))
    } else app.showError(error?.message || t('admin.rolePermissions.saveFailed'))
  } finally { saving.value = false }
}
onMounted(load)
</script>
