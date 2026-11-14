<template>
  <section v-if="auth.isSuperAdmin" class="card overflow-hidden" data-testid="admin-role-permissions">
    <header class="px-4 py-5 sm:px-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.rolePermissions.title') }}</h2>
      <p class="mt-1 text-xs text-gray-500">{{ t('admin.rolePermissions.description') }}</p>
    </header>
    <div v-if="loading" class="px-6 pb-6 text-sm">{{ t('common.loading') }}</div>
    <template v-else-if="policy">
      <p v-if="policy.version === 0" role="alert" class="mx-4 mb-4 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">{{ t('admin.rolePermissions.recovery') }}</p>
      <p v-if="!stepUpEnabled" role="alert" class="mx-4 mb-4 rounded-lg bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-200">{{ t('admin.rolePermissions.stepUpDisabled') }}</p>
      <div v-if="conflict" role="alert" class="mx-4 mb-4 space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
        <p>{{ t('admin.rolePermissions.conflict') }}</p>
        <button type="button" class="btn btn-secondary" :disabled="busy || leaveResolver !== null" data-testid="reconcile" @click="reconcile">{{ t('admin.rolePermissions.reconcile') }}</button>
      </div>
      <label class="block border-b border-gray-200 px-4 pb-3 dark:border-dark-700 sm:hidden">
        <span class="sr-only">{{ t('admin.rolePermissions.categoriesLabel') }}</span>
        <select class="input" :disabled="busy || leaveResolver !== null" :value="currentCategory" @change="activeCategory = ($event.target as HTMLSelectElement).value">
          <option v-for="category in categories" :key="category.key" :value="category.key">{{ t(`admin.rolePermissions.categories.${category.key}`) }}</option>
        </select>
      </label>
      <nav class="hidden flex-wrap gap-x-5 sm:flex border-b border-gray-200 px-4 dark:border-dark-700 sm:px-6" :aria-label="t('admin.rolePermissions.categoriesLabel')">
        <button v-for="category in categories" :key="category.key" type="button" :aria-pressed="currentCategory === category.key" :data-category="category.key" :disabled="busy || leaveResolver !== null"
          class="border-b-2 px-0 py-3 text-sm" :class="currentCategory === category.key ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500'" @click="activeCategory = category.key">{{ t(`admin.rolePermissions.categories.${category.key}`) }}</button>
      </nav>
      <div class="permission-row hidden bg-gray-50 text-xs text-gray-500 dark:bg-dark-900 sm:grid" aria-hidden="true">
        <span>{{ t('admin.rolePermissions.module') }}</span><span>{{ t('admin.rolePermissions.basic') }}</span><span>{{ t('admin.rolePermissions.extra') }}</span><span />
      </div>
      <section v-for="category in categories" v-show="currentCategory === category.key" :key="category.key" :aria-label="t(`admin.rolePermissions.categories.${category.key}`)">
        <div v-for="group in category.modules" :key="group.module" class="border-b border-gray-100 dark:border-dark-700">
          <div class="permission-row">
            <span class="text-sm font-medium">{{ moduleLabel(group.module) }}</span>
            <select v-if="basicPermissions(group.permissions).length" :data-module="group.module" :aria-label="moduleLabel(group.module)" :value="moduleLevel(group.permissions)" :disabled="editingBlocked" class="input !py-1.5" @change="setModuleLevel(group.permissions, ($event.target as HTMLSelectElement).value)">
              <option value="off">{{ t('admin.rolePermissions.levels.off') }}</option>
              <option value="read">{{ t('admin.rolePermissions.levels.read') }}</option>
              <option v-if="basicPermissions(group.permissions).some(p => !p.key.endsWith('.read'))" value="manage">{{ t('admin.rolePermissions.levels.manage') }}</option>
              <option v-if="moduleLevel(group.permissions) === 'custom'" value="custom">{{ t('admin.rolePermissions.custom') }}</option>
            </select>
            <span v-else class="text-xs text-gray-500">{{ t('admin.rolePermissions.individual') }}</span>
            <span class="permission-summary text-xs text-gray-500" :class="extraSummary(group.permissions) === '—' ? 'hidden sm:block' : ''">{{ extraSummary(group.permissions) }}</span>
            <button type="button" :data-expand="group.module" :disabled="busy || leaveResolver !== null" :aria-expanded="expanded === group.module" :aria-controls="`permission-detail-${group.module}`" class="permission-expand py-1 text-right text-xs text-primary-600 dark:text-primary-400" @click="expanded = expanded === group.module ? '' : group.module">{{ t(expanded === group.module ? 'admin.rolePermissions.collapse' : 'admin.rolePermissions.details') }}</button>
          </div>
          <div v-show="expanded === group.module" :id="`permission-detail-${group.module}`" class="flex flex-wrap gap-x-6 gap-y-3 bg-gray-50 px-4 py-4 dark:bg-dark-900 sm:px-6">
            <label v-for="permission in group.permissions" :key="permission.key" class="flex items-center gap-2 text-sm">
              <input type="checkbox" :data-permission="permission.key" :checked="selected.includes(permission.key)" :disabled="editingBlocked" @change="toggle(permission, ($event.target as HTMLInputElement).checked)" />
              <span>{{ actionLabel(permission) }}</span>
              <span v-if="permission.sensitive && stepUpEnabled" class="rounded border border-gray-200 px-1 text-[11px] text-gray-500 dark:border-dark-600">{{ t('admin.rolePermissions.sensitive') }}</span>
            </label>
          </div>
        </div>
      </section>
      <footer class="flex flex-wrap items-center gap-3 px-4 py-4 sm:px-6">
        <span class="mr-auto text-xs text-gray-500" aria-live="polite">{{ t(dirty ? 'admin.rolePermissions.pending' : 'admin.rolePermissions.unchanged', { count: added.length + removed.length }) }}</span>
        <button type="button" class="btn btn-secondary" :disabled="busy || leaveResolver !== null || !hasDraft" data-testid="discard" @click="discard">{{ t('admin.rolePermissions.discard') }}</button>
        <button type="button" class="btn btn-primary" :disabled="editingBlocked || !dirty" data-testid="review" @click="openReview">{{ t('admin.rolePermissions.review') }}</button>
      </footer>
    </template>
    <button v-else type="button" class="btn btn-secondary m-4" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
    <BaseDialog :show="reviewing" :title="t('admin.rolePermissions.reviewTitle')" width="narrow" :show-close-button="!busy" :close-on-escape="!busy" @close="closeReview">
      <div class="space-y-4 text-sm">
        <p>{{ t('admin.rolePermissions.affected', { count: affectedAdminCount }) }}</p>
        <div class="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-900" data-testid="permission-preview">
          <p v-for="key in added" :key="'add-' + key">＋ {{ t('admin.rolePermissions.added') }} · {{ permissionLabel(key) }}</p>
          <p v-for="key in removed" :key="'remove-' + key">− {{ t('admin.rolePermissions.removed') }} · {{ permissionLabel(key) }}</p>
          <p v-if="!added.length && !removed.length">{{ t('admin.rolePermissions.noChanges') }}</p>
        </div>
        <label class="block">{{ t('admin.rolePermissions.reason') }}<textarea v-model="reason" maxlength="512" class="input mt-2 w-full" :disabled="busy || conflict || leaveResolver !== null" rows="2" /></label>
        <p class="text-xs text-gray-500">{{ t('admin.rolePermissions.sessionNotice') }}</p>
      </div>
      <template #footer><div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy || leaveResolver !== null" @click="closeReview">{{ t('admin.rolePermissions.continueEditing') }}</button>
        <button type="button" class="btn btn-primary" data-testid="save" :disabled="busy || conflict || leaveResolver !== null || !dirty || !reason.trim()" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div></template>
    </BaseDialog>
    <ConfirmDialog :show="leaveResolver !== null" :title="t('admin.rolePermissions.discard')" :message="t('admin.rolePermissions.leaveWarning')" :confirm-text="t('admin.rolePermissions.discard')" :cancel-text="t('admin.rolePermissions.continueEditing')" @confirm="resolveLeave(true)" @cancel="resolveLeave(false)" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { isCancel } from 'axios'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import { readApiError } from '@/shared/lib/apiError'
import { useAuthStore } from '../model/auth'
import { useIdentityNotifications } from '../ports'
import { useIdentityRequest } from '../queries/useIdentityRequest'
import { getAdminRolePermissions, updateAdminRolePermissions, type AdminPermission, type AdminRolePolicy } from '../api/roles'

const { t } = useI18n()
const auth = useAuthStore()
const app = useIdentityNotifications()
const requestContext = () => [auth.identityScopeKey, auth.isSuperAdmin]
const reads = useIdentityRequest(requestContext)
const writes = useIdentityRequest(requestContext)
const leaves = useIdentityRequest(requestContext)
const discards = useIdentityRequest(requestContext)
const activeCategory = ref('resources')
const expanded = ref('')
const reviewing = ref(false)
const conflict = ref(false)
const leaveResolver = ref<((value: boolean) => void) | null>(null)
const loading = ref(false)
const reconciling = ref(false)
const saving = ref(false)
const policy = ref<AdminRolePolicy | null>(null)
const catalogue = ref<AdminPermission[]>([])
const selected = ref<string[]>([])
const reason = ref('')
const affectedAdminCount = ref(0)
const stepUpEnabled = ref(true)
const busy = computed(() => loading.value || saving.value || reconciling.value)
const editingBlocked = computed(() => !auth.isSuperAdmin || busy.value || conflict.value || reviewing.value || leaveResolver.value !== null)
const categories = computed(() => ['personnel', 'resources', 'finance', 'operations', 'settings'].map(key => {
  const modules = new Map<string, AdminPermission[]>()
  for (const item of catalogue.value.filter(p => p.group === key)) modules.set(item.module, [...(modules.get(item.module) || []), item])
  return { key, modules: [...modules].map(([module, permissions]) => ({ module, permissions })) }
}).filter(category => category.modules.length))
const currentCategory = computed(() => categories.value.some(c => c.key === activeCategory.value) ? activeCategory.value : categories.value[0]?.key)
const moduleLabel = (module: string) => t(`admin.rolePermissions.modules.${module.replace(/\./g, '_')}`)
const actionLabel = (permission: AdminPermission) => t(`admin.rolePermissions.actions.${permission.key.slice(permission.module.length + 1).replace(/\./g, '_')}`)
function extraSummary(permissions: AdminPermission[]) {
  const extra = permissions.filter(p => !basicPermissions(permissions).includes(p))
  return extra.filter(p => selected.value.includes(p.key)).map(actionLabel).join(' · ') || (extra.length ? t('admin.rolePermissions.notGranted') : '—')
}
const added = computed(() => selected.value.filter(key => !policy.value?.permissions.includes(key)))
const removed = computed(() => (policy.value?.permissions || []).filter(key => !selected.value.includes(key)))
const dirty = computed(() => policy.value?.version === 0 || added.value.length > 0 || removed.value.length > 0)
const hasDraft = computed(() => dirty.value || conflict.value || !!reason.value.trim())
function permissionLabel(key: string) {
  const permission = catalogue.value.find(item => item.key === key)
  return permission ? `${moduleLabel(permission.module)} · ${actionLabel(permission)}` : key
}
function basicPermissions(permissions: AdminPermission[]) {
  return permissions.filter(p => !p.sensitive && ['read', 'manage', 'create', 'update', 'delete'].includes(p.key.slice(p.module.length + 1)))
}
function moduleLevel(permissions: AdminPermission[]) {
  const basic = basicPermissions(permissions)
  const granted = basic.filter(p => selected.value.includes(p.key))
  if (!granted.length) return 'off'
  if (granted.length === 1 && granted[0].key.endsWith('.read')) return 'read'
  return granted.length === basic.length ? 'manage' : 'custom'
}
function setModuleLevel(permissions: AdminPermission[], level: string) {
  if (editingBlocked.value || !['off', 'read', 'manage'].includes(level)) return
  for (const p of basicPermissions(permissions).slice().reverse()) {
    if (level === 'off' || (level === 'read' && !p.key.endsWith('.read'))) toggle(p, false)
  }
  for (const p of basicPermissions(permissions)) if (level === 'manage' || (level === 'read' && p.key.endsWith('.read'))) toggle(p, true)
}

async function load() {
  // Only initial/retry loads may replace the editor. Conflicts use explicit reconciliation.
  if (!auth.isSuperAdmin || busy.value || policy.value) return
  const ticket = reads.begin()
  if (!ticket.isCurrent()) return
  loading.value = true
  try {
    const response = await getAdminRolePermissions({ signal: ticket.signal })
    if (!ticket.isCurrent()) return
    affectedAdminCount.value = response.affected_admin_count ?? affectedAdminCount.value
    stepUpEnabled.value = response.step_up_enabled ?? stepUpEnabled.value
    policy.value = response.policy
    catalogue.value = response.catalogue
    selected.value = [...response.policy.permissions]
  } catch (error) {
    if (ticket.isCurrent() && !isCancel(error)) app.showError(t('admin.rolePermissions.loadFailed'))
  } finally { if (ticket.isCurrent()) loading.value = false }
}
// Both direct edits and reconciliation use only the server's dependency catalogue.
function changeGrant(next: Set<string>, permissions: AdminPermission[], key: string, checked: boolean) {
  const add = (value: string) => {
    if (next.has(value)) return
    const permission = permissions.find(item => item.key === value)
    if (!permission) return
    next.add(value)
    for (const dependency of permission.dependencies) add(dependency)
  }
  if (checked) add(key)
  else {
    next.delete(key)
    let changed = true
    while (changed) {
      changed = false
      for (const item of permissions) {
        if (next.has(item.key) && item.dependencies.some(dependency => !next.has(dependency))) {
          next.delete(item.key)
          changed = true
        }
      }
    }
  }
}
function toggle(permission: AdminPermission, checked: boolean) {
  if (editingBlocked.value) return
  const next = new Set(selected.value)
  changeGrant(next, catalogue.value, permission.key, checked)
  selected.value = [...next].sort()
}
function openReview() {
  if (!editingBlocked.value && dirty.value) reviewing.value = true
}
function closeReview() {
  if (!busy.value && !leaveResolver.value) reviewing.value = false
}
async function confirmLeave(): Promise<boolean> {
  if (busy.value || leaveResolver.value) return false
  const ticket = leaves.begin()
  if (!ticket.isCurrent()) return false
  if (!hasDraft.value) return true
  const confirmed = await new Promise<boolean>(resolve => { leaveResolver.value = resolve })
  return confirmed && ticket.isCurrent() && !busy.value
}
function resolveLeave(confirmed: boolean) {
  const resolve = leaveResolver.value
  leaveResolver.value = null
  resolve?.(confirmed)
}
async function discard() {
  if (!auth.isSuperAdmin || busy.value || leaveResolver.value) return
  const ticket = discards.begin()
  if (!await confirmLeave() || !ticket.isCurrent() || busy.value) return
  selected.value = [...(policy.value?.permissions || [])]
  reason.value = ''
  reviewing.value = false
  // An unresolved conflict still needs the latest server baseline before saving.
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (!hasDraft.value && !saving.value && !reconciling.value) return
  event.preventDefault()
  event.returnValue = ''
}
async function reconcile() {
  if (!auth.isSuperAdmin || busy.value || leaveResolver.value || !conflict.value) return
  const ticket = reads.begin()
  if (!ticket.isCurrent()) return
  const localAdded = [...added.value]
  const localRemoved = [...removed.value]
  reconciling.value = true
  try {
    const response = await getAdminRolePermissions({ signal: ticket.signal })
    if (!ticket.isCurrent()) return
    // Keep concurrent grants, including server grants absent from this catalogue.
    // Replay only explicit local changes; dependency side effects remain visible in review.
    const next = new Set(response.policy.permissions)
    for (const key of localRemoved) changeGrant(next, response.catalogue, key, false)
    for (const key of localAdded) changeGrant(next, response.catalogue, key, true)
    catalogue.value = response.catalogue
    policy.value = response.policy
    affectedAdminCount.value = response.affected_admin_count ?? affectedAdminCount.value
    stepUpEnabled.value = response.step_up_enabled ?? stepUpEnabled.value
    selected.value = [...next].sort()
    conflict.value = false
    reviewing.value = true
  } catch (error) {
    if (ticket.isCurrent() && !isCancel(error)) app.showError(t('admin.rolePermissions.loadFailed'))
  } finally { if (ticket.isCurrent()) reconciling.value = false }
}
async function save() {
  if (!auth.isSuperAdmin || !policy.value || busy.value || conflict.value || leaveResolver.value || !reviewing.value || !dirty.value || !reason.value.trim()) return
  const ticket = writes.begin()
  if (!ticket.isCurrent()) return
  const version = policy.value.version
  const permissions = [...selected.value]
  const changeReason = reason.value.trim()
  saving.value = true
  try {
    const response = await updateAdminRolePermissions(version, permissions, changeReason, { signal: ticket.signal })
    if (!ticket.isCurrent()) return
    policy.value = response.policy
    selected.value = [...response.policy.permissions]
    reason.value = ''
    reviewing.value = false
    app.showSuccess(t('admin.rolePermissions.saved'))
  } catch (error: unknown) {
    if (!ticket.isCurrent() || isCancel(error)) return
    const errorDetails = readApiError(error)
    if (errorDetails.reason === 'ADMIN_POLICY_CONFLICT') {
      conflict.value = true
      reviewing.value = false
      app.showError(t('admin.rolePermissions.conflict'))
    } else app.showError(errorDetails.message || t('admin.rolePermissions.saveFailed'))
  } finally { if (ticket.isCurrent()) saving.value = false }
}
function clearScope(): void {
  reads.cancel(); writes.cancel(); leaves.cancel(); discards.cancel()
  resolveLeave(false)
  policy.value = null; catalogue.value = []; selected.value = []; reason.value = ''
  loading.value = false; saving.value = false; reconciling.value = false
  affectedAdminCount.value = 0; stepUpEnabled.value = true
  conflict.value = false; reviewing.value = false; expanded.value = ''; activeCategory.value = 'resources'
}
defineExpose({ confirmLeave })
onBeforeRouteLeave(confirmLeave)
watch(requestContext, () => { clearScope(); void load() }, { immediate: true, flush: 'sync' })
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onScopeDispose(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  clearScope()
})
</script>

<style scoped>
.permission-row { display: grid; grid-template-columns: minmax(120px, 1fr) 150px minmax(120px, 1fr) 64px; gap: 16px; align-items: center; padding: 12px 24px; }
@media (max-width: 639px) {
  .permission-row { grid-template-columns: minmax(0, 1fr) 120px 56px; gap: 8px; padding: 12px 16px; }
  .permission-row[aria-hidden] { display: none; }
  .permission-summary { grid-column: 1 / 3; grid-row: 2; }
  .permission-expand { grid-column: 3; grid-row: 1; }
}
@media (pointer: coarse) { button, label, select { min-height: 44px; } }
</style>
