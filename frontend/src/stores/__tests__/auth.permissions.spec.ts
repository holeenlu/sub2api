import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import type { User } from '@/types'

beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()) })
describe('three fixed management roles', () => {
  it('supports a super administrator without any ordinary administrators', () => {
    const auth = useAuthStore()
    auth.user = { id: 1, role: 'super_admin' } as User
    expect(auth.isAdmin).toBe(true)
    expect(auth.can('accounts.authorize')).toBe(true)
    expect(auth.canAccessAdminPage('/admin/settings')).toBe(true)
    expect(auth.adminLandingPath).toBe('/admin/dashboard')
  })
  it('uses the server snapshot and never widens a page grant to sibling configuration', () => {
    const auth = useAuthStore()
    auth.user = { id: 2, role: 'admin', policy_version:2, permissions:['orders.read'], admin_pages:['/admin/orders'] } as User
    expect(auth.isAdmin).toBe(true)
    expect(auth.isSuperAdmin).toBe(false)
    expect(auth.can('orders.read')).toBe(true)
    expect(auth.can('orders.refund')).toBe(false)
    expect(auth.canAccessAdminPage('/admin/orders?page=2')).toBe(true)
    expect(auth.canAccessAdminPage('/admin/orders/plans')).toBe(false)
    expect(auth.canAccessAdminPage('/admin/settings')).toBe(false)
    expect(auth.adminLandingPath).toBe('/admin/orders')
    Object.assign(auth.user,{policy_version:3,permissions:[],admin_pages:[]})
    expect(auth.can('orders.read')).toBe(false)
    expect(auth.adminLandingPath).toBe('/dashboard')
  })
  it('fails closed for missing admin permissions and regular users', () => {
    const auth = useAuthStore()
    for (const role of ['admin', 'user'] as const) {
      auth.user = { id: 2, role } as User
      expect(auth.can('users.update')).toBe(false)
      expect(auth.canAccessAdminPage('/admin/users')).toBe(false)
    }
  })
})

it('uses server field metadata for display and payloads without inferring financial names', () => {
  const auth = useAuthStore()
  auth.user = { id: 2, role: 'admin', admin_write_fields: { 'group.update': ['name', 'status'] } } as User
  expect(auth.canEditAdminField('group.update', 'peak_start')).toBe(false)
  expect(auth.editableAdminFields('group.update', { name: 'renamed', peak_start: '09:00', rate_multiplier: 1 })).toEqual({ name: 'renamed' })
  auth.user.role = 'super_admin'
  expect(auth.canEditAdminField('group.update', 'peak_start')).toBe(true)
})
