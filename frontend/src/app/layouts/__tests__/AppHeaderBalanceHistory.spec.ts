import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/features/identity'
import type { User } from '@/features/identity/api/profileTypes';
import AppHeader from '../AppHeader.vue'
import { layoutRouter } from './layoutTestFixture'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

describe('personal recharge menu', () => {
  it.each(['user', 'admin', 'super_admin'] as const)('opens the same self-only dialog for %s', async role => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().user = { id: 12, role, email: 'me@example.com', balance: 1 } as User
    const router = await layoutRouter('/keys')
    const wrapper = mount(AppHeader, { global: {
      plugins: [pinia, router], mocks: { $t: (key: string) => key },
      stubs: { RouterLink: true, Icon: true, LocaleSwitcher: true, SubscriptionProgressMini: true, AnnouncementBell: true,
        BalanceHistoryModal: { props: ['show'], template: '<div v-if="show" data-testid="personal-history" />' }
      }
    } })
    await wrapper.get('[aria-label="common.userMenu"]').trigger('click')
    await wrapper.get('[data-testid="balance-history-menu"]').trigger('click')
    expect(wrapper.find('[data-testid="personal-history"]').exists()).toBe(true)
    // The menu remains in the DOM until its exit animation finishes.
    expect(wrapper.get('.dropdown').classes()).toContain('dropdown-leave-active')
  })
})
