import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AdminRolePermissions from '../AdminRolePermissions.vue'

const mocks = vi.hoisted(() => ({
  root: { isSuperAdmin: true }, get: vi.fn(), update: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.root }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/api/admin/roles', () => ({ getAdminRolePermissions: mocks.get, updateAdminRolePermissions: mocks.update }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
const catalogue = [
  { key: 'accounts.read', module: 'accounts', group:'resources', dependencies: [], sensitive: false },
  { key: 'accounts.manage', module: 'accounts', group:'resources', dependencies: ['accounts.read'], sensitive: false },
  { key: 'accounts.authorize', module: 'accounts', group:'resources', dependencies: ['accounts.read', 'accounts.manage'], sensitive: true },
]
const response = (version = 7) => ({ policy: { version, permissions: [] }, catalogue, step_up_enabled:true, affected_admin_count:3 })
const render = () => mount(AdminRolePermissions)
beforeEach(() => { vi.clearAllMocks(); mocks.root.isSuperAdmin = true; mocks.get.mockResolvedValue(response()); mocks.update.mockResolvedValue(response(8)) })
describe('one shared administrator policy', () => {
  it('uses server dependencies and submits the loaded version plus a reason', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-permission="accounts.authorize"]').setValue(true)
    expect((wrapper.get('[data-module="accounts"]').element as HTMLSelectElement).value).toBe('manage')
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')!
    expect(save.attributes('disabled')).toBeDefined()
    await wrapper.get('textarea').setValue('Enable the authorized operations team')
    await save.trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, ['accounts.authorize', 'accounts.manage', 'accounts.read'], 'Enable the authorized operations team')
    wrapper.unmount()
  })
  it('removing a prerequisite also removes its dependent grants', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-permission="accounts.authorize"]').setValue(true)
    await wrapper.get('[data-module="accounts"]').setValue('off')
    expect(wrapper.findAll('input:checked')).toHaveLength(0)
    wrapper.unmount()
  })
  it('allows explicit empty-policy recovery without default grants', async () => {
    mocks.get.mockResolvedValue(response(0))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.rolePermissions.recovery')
    await wrapper.get('textarea').setValue('Recover without granting any operational access')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(0, [], 'Recover without granting any operational access')
    wrapper.unmount()
  })
  it('keeps grants depending only on read when manage is downgraded', async () => {
    const extra = {key:'accounts.diagnostics',module:'accounts',group:'resources',dependencies:['accounts.read'],sensitive:false}
    mocks.get.mockResolvedValue({...response(),catalogue:[...catalogue,extra],policy:{version:7,permissions:['accounts.read','accounts.manage','accounts.authorize','accounts.diagnostics']}})
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    expect((wrapper.get('[data-permission="accounts.diagnostics"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-permission="accounts.authorize"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('[data-module="accounts"]').setValue('off')
    expect((wrapper.get('[data-permission="accounts.diagnostics"]').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })
  it('keeps staff management and balance adjustment when user management becomes read-only', async () => {
    const grants = [
      {key:'users.read',module:'users',group:'personnel',dependencies:[],sensitive:false},
      {key:'users.update',module:'users',group:'personnel',dependencies:['users.read'],sensitive:false},
      {key:'staff.manage',module:'staff',group:'personnel',dependencies:['users.read'],sensitive:true},
      {key:'billing.balance.adjust',module:'billing',group:'finance',dependencies:['users.read'],sensitive:true}
    ]
    mocks.get.mockResolvedValue({...response(),catalogue:grants,policy:{version:7,permissions:grants.map(p=>p.key)}})
    const wrapper = render();await flushPromises()
    await wrapper.get('[data-module="users"]').setValue('read')
    expect((wrapper.get('[data-permission="staff.manage"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-permission="billing.balance.adjust"]').element as HTMLInputElement).checked).toBe(true)
    wrapper.unmount()
  })
  it('recognizes the structured conflict reason and reloads the current policy', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    await wrapper.get('textarea').setValue('A concurrent administrator saved first')
    mocks.update.mockRejectedValue({status:409,code:409,reason:'ADMIN_POLICY_CONFLICT'})
    mocks.get.mockResolvedValue({...response(9),policy:{version:9,permissions:['accounts.read']}})
    await wrapper.findAll('button').find(button => button.text()==='common.save')!.trigger('click');await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.showError).toHaveBeenCalledWith('admin.rolePermissions.conflict')
    expect(wrapper.text()).toContain('admin.rolePermissions.version')
    wrapper.unmount()
  })
  it.each([true, false])('saves directly without TOTP when site step-up is %s', async (enabled) => {
    mocks.get.mockResolvedValue({ ...response(), step_up_enabled: enabled })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    await wrapper.get('textarea').setValue('Allow operations visibility')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, ['accounts.read'], 'Allow operations visibility')
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.rolePermissions.saved')
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('never loads or renders the role editor for a limited administrator', async () => {
    mocks.root.isSuperAdmin = false
    const wrapper = render(); await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="admin-role-permissions"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
