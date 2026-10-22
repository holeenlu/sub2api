import type { User } from '@/types'

// Keep user-management selectors in a stable display order, independent of IDs.
export const USER_ROLES = [
  'super_admin',
  'admin',
  'user',
  'vendor_level1',
  'vendor_level2'
] as const satisfies readonly User['role'][]

export const isVendorRole = (role: User['role'] | undefined): boolean =>
  role === 'vendor_level1' || role === 'vendor_level2'
