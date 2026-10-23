import apiClient from '../client'

export interface AdminRolePolicy {
  version: number
  permissions: string[]
  updated_by: number
  updated_at: string
}
export interface AdminPermission {
  key: string
  module: string
  label: string
  dependencies: string[]
  sensitive: boolean
  aliases: string[]
  group: string
}
export interface AdminRolePermissionsResponse {
  policy: AdminRolePolicy
  catalogue: AdminPermission[]
  affected_admin_count?: number
  step_up_enabled?: boolean
}
export const getAdminRolePermissions = async () => {
  const { data } = await apiClient.get<AdminRolePermissionsResponse>('/admin/roles/admin/permissions')
  return data
}
export const updateAdminRolePermissions = async (expectedVersion: number, permissions: string[], reason: string) => {
  const { data } = await apiClient.put<AdminRolePermissionsResponse>('/admin/roles/admin/permissions', {
    expected_version: expectedVersion,
    permissions,
    reason,
  })
  return data
}
