import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/features/identity'
import type { User } from '@/features/identity/api/profileTypes'
import { installResourceServices } from '@/app/compositions/resourceServices'
import { useAppStore } from '@/app/model/app'
import { useSupplyStore } from '@/features/supply'
import { useResourceEditorServices } from '../services'

function restrictedAdmin(permissions: string[]): User {
  return {
    id: 42, username: 'restricted-admin', email: 'fixture@example.test', role: 'admin',
    permissions, policy_version: 1, admin_write_fields: { 'account.update': ['name'] },
    balance: 0, concurrency: 1, status: 'active', allowed_groups: [],
    balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [],
    created_at: '2026-10-28T00:00:00Z', updated_at: '2026-10-28T00:00:00Z'
  }
}
function setup(permissions: string[]) {
  const app = createApp({ render: () => null })
  const auth = useAuthStore()
  auth.user = restrictedAdmin(permissions)
  auth.token = 'fixture-session-token'
  installResourceServices(app)
  return { auth, services: app.runWithContext(useResourceEditorServices) }
}
beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()) })
afterEach(() => {
  useAuthStore().$dispose(); useAppStore().$dispose(); useSupplyStore().$dispose()
  localStorage.clear(); vi.unstubAllEnvs()
})

describe('resource model selector permission composition', () => {
  it('allows a restricted administrator with the real accounts.manage grant to edit independent references', () => {
    const { auth, services } = setup(['accounts.read', 'accounts.manage'])
    expect(auth.isSuperAdmin).toBe(false)
    expect(services.access.canManage).toBe(true)
    expect(services.access.canReadModelCatalog).toBe(true)
    expect(services.access.canAuthorize).toBe(false)
    expect(services.access.canEditAdminField?.('account.update', 'credentials')).toBe(false)
    expect(auth.editableAdminFields('account.update', { name: 'updated', credentials: { model_allowlist: ['seed'] } })).toEqual({ name: 'updated' })
  })
  it('does not grant account model writes from accounts.read or a risk-only grant', () => {
    const { auth, services } = setup(['accounts.read', 'risk.read', 'risk.manage'])
    expect(services.access.canReadModelCatalog).toBe(true)
    expect(services.access.canManage).toBe(false)
    auth.user = restrictedAdmin(['risk.read', 'risk.manage'])
    expect(services.access.canReadModelCatalog).toBe(false)
    expect(services.access.canManage).toBe(false)
  })
  it('keeps management, credential authorization and credentials field permissions independent', () => {
    const { auth, services } = setup(['accounts.read', 'accounts.manage', 'accounts.authorize'])
    expect(services.access.canManage).toBe(true)
    expect(services.access.canAuthorize).toBe(true)
    expect(services.access.canEditAdminField?.('account.update', 'credentials')).toBe(false)
    auth.user = { ...restrictedAdmin(['accounts.read', 'accounts.manage', 'accounts.authorize']), admin_write_fields: { 'account.update': ['name', 'credentials'] } }
    expect(services.access.canEditAdminField?.('account.update', 'credentials')).toBe(true)
    auth.user = restrictedAdmin(['accounts.read'])
    expect(services.access.canManage).toBe(false)
    expect(services.access.canAuthorize).toBe(false)
  })
  it('keeps supplier portal bindings away from the admin model APIs even with account grants', () => {
    vi.stubEnv('VITE_VENDOR_PORTAL', 'true')
    const { services } = setup(['accounts.read', 'accounts.manage', 'accounts.authorize'])
    expect(services.access.canManage).toBe(false)
    expect(services.access.canReadModelCatalog).toBe(false)
  })
})
