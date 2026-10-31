import { config } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { vi } from 'vitest'
import { announcementServicesKey, type AnnouncementServices } from '../announcementPorts'
export function installAnnouncementTestServices(overrides: Partial<AnnouncementServices> = {}) {
  const services: AnnouncementServices = {
    scopeKey: ref('session-a:user'), canManage: ref(true),
    notifySuccess: vi.fn(), notifyError: vi.fn(), listSubscriptionGroups: vi.fn().mockResolvedValue([]),
    GroupSelector: defineComponent({ props: ['modelValue', 'groups'], emits: ['update:modelValue'], setup(_, { emit }) { return () => h('button', { 'data-testid': 'groups', onClick: () => emit('update:modelValue', [2]) }, 'Choose group') } }),
    ...overrides,
  }
  config.global.provide = { ...config.global.provide, [announcementServicesKey as symbol]: services }
  return services
}
