import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserActionMenu from '../UserActionMenu.vue'
import UserRowActions from '../UserRowActions.vue'
import type { AdminUser } from '../../../api/profileTypes'

const actor = vi.hoisted(() => ({ isSuperAdmin: true, user: { id: 1 }, permissions: ['users.update', 'api_keys.read'] }))
vi.mock('../../../model/auth', () => ({ useAuthStore: () => ({
  ...actor, can: (permission: string) => actor.isSuperAdmin || actor.permissions.includes(permission)
}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => { actor.isSuperAdmin = true; actor.permissions = ['users.update', 'api_keys.read'] })
const member = (id: number, role: AdminUser['role'] = 'user') => ({ id, role, status: 'active' } as AdminUser)
const menu = (user: AdminUser) => mount(UserActionMenu, {
  props: { users: [user], activeMenuId: user.id, menuPosition: { top: 10, left: 10 } },
  global: { stubs: { Teleport: true, Icon: true } }
})

describe('user management action boundaries', () => {
  it('preserves peer super-admin management while excluding protected balance actions', () => {
    const wrapper = menu(member(2, 'super_admin'))
    expect(wrapper.text()).toContain('common.delete')
    expect(wrapper.text()).toContain('admin.users.apiKeys')
    expect(wrapper.text()).not.toContain('admin.users.deposit')
    expect(wrapper.text()).not.toContain('admin.users.withdraw')
    wrapper.unmount()
  })
  it('does not expose self deletion or self balance adjustment', () => {
    const wrapper = menu(member(1, 'super_admin'))
    expect(wrapper.text()).not.toContain('common.delete')
    expect(wrapper.text()).not.toContain('admin.users.deposit')
    expect(wrapper.text()).not.toContain('admin.users.withdraw')
    wrapper.unmount()
  })
  it('does not expand ordinary administrators to peer administrator modification', () => {
    actor.isSuperAdmin = false
    const wrapper = mount(UserRowActions, { props: { user: member(2, 'admin'), activeMenuId: null }, global: { stubs: { Icon: true } } })
    expect(wrapper.text()).not.toContain('common.edit')
    expect(wrapper.text()).not.toContain('admin.users.disable')
    expect(wrapper.text()).toContain('common.more')
    wrapper.unmount()
  })
  it('emits the selected user to the app quota composition and closes the menu', async () => {
    const user = member(7)
    const wrapper = menu(user)
    await wrapper.findAll('button').find(button => button.text() === 'admin.users.platformQuota.menuItem')!.trigger('click')
    expect(wrapper.emitted('platformQuota')).toEqual([[user]])
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
