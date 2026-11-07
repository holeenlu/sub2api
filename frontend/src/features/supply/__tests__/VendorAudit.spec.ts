import { expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import VendorAudit from '../components/VendorAudit.vue'
import StatusBadge from '@/shared/ui/StatusBadge.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages

const api = vi.hoisted(() => ({ audit: vi.fn() }))
vi.mock('../api', () => ({ vendorAPI: api }))

it('preserves safe audit result summaries instead of replacing export counts with unknown', async () => {
  const results = [
    'success',
    'failed',
    'rows=7',
    'success:count=2',
    'created=1,duplicate=1,invalid=0',
  ]
  api.audit.mockResolvedValue({
    items: results.map((result, index) => ({
      id: index + 1,
      created_at: '2026-10-15T12:00:00Z',
      actor_user_id: 9,
      actor_role: 'super_admin',
      action: index < 2 ? 'account.export' : 'usage.export',
      vendor_id: 1,
      object_id: '0',
      result,
    })),
    total: results.length,
  })
  const wrapper = mount(VendorAudit, {
    props: { admin: true, vendorId: 1 },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } }),
      ],
      stubs: { Pagination: true },
    },
  })
  await flushPromises()
  expect(
    wrapper.findAllComponents(StatusBadge).map((badge) => badge.props('label')),
  ).toEqual([
    nativeEn.vendor.success,
    nativeEn.vendor.failed,
    ...results.slice(2),
  ])
  expect(wrapper.findAllComponents(StatusBadge)[1].props('status')).toBe(
    'error',
  )
  expect(api.audit).toHaveBeenCalledWith(
    true,
    expect.objectContaining({ vendor_id: 1 }),
    expect.any(AbortSignal),
  )
  wrapper.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
