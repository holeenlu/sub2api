import { beforeEach, describe, expect, it, vi } from 'vitest'
const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/shared/http', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/shared/http')>()), apiClient: http
}))
import { changePassword } from '../profile'
import { getAdminRolePermissions, updateAdminRolePermissions } from '../roles'
beforeEach(() => {
  vi.resetAllMocks()
  http.get.mockResolvedValue({ data: {} })
  http.put.mockResolvedValue({ data: { message: 'ok' } })
})
describe('identity request cancellation contracts', () => {
  it.each([undefined, { signal: new AbortController().signal }])('preserves password payload with optional config %j', async options => {
    await changePassword('old-synthetic-password', 'new-synthetic-password', options)
    expect(http.put.mock.calls[0][0]).toBe('/user/password')
    expect(http.put.mock.calls[0][1]).toEqual({ old_password: 'old-synthetic-password', new_password: 'new-synthetic-password' })
    expect(http.put.mock.calls[0][2]).toBe(options)
  })
  it('forwards cancellation without weakening the role policy version check', async () => {
    const options = { signal: new AbortController().signal }
    await getAdminRolePermissions(options)
    await updateAdminRolePermissions(17, ['users.read'], 'audited reason', options)
    expect(http.get).toHaveBeenCalledWith('/admin/roles/admin/permissions', options)
    expect(http.put).toHaveBeenCalledWith('/admin/roles/admin/permissions', {
      expected_version: 17, permissions: ['users.read'], reason: 'audited reason'
    }, options)
  })
})
