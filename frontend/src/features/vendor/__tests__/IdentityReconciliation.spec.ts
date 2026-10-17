import { beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import IdentityReconciliation from '../components/IdentityReconciliation.vue'
import en from '@/i18n/locales/en/vendor'
const api = vi.hoisted(() => ({ reconcileIdentities: vi.fn() }))
const auth = vi.hoisted(() => ({ isSuperAdmin: true }))
vi.mock('../api', () => ({ vendorAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
function render() {
  return mount(IdentityReconciliation, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { vendor: en } },
        }),
      ],
    },
  })
}
beforeEach(() => {
  vi.clearAllMocks()
  auth.isSuperAdmin = true
})
it('makes no automatic provider calls and hides controls from non-superadmins', async () => {
  const w = render()
  await flushPromises()
  expect(api.reconcileIdentities).not.toHaveBeenCalled()
  expect(w.text()).toContain(en.reconcileHelp)
  w.unmount()
  auth.isSuperAdmin = false
  const denied = render()
  expect(denied.find('button').exists()).toBe(false)
  expect(api.reconcileIdentities).not.toHaveBeenCalled()
  denied.unmount()
})
it('advances one bounded batch per click, retains failed rows, and retries their cursor independently', async () => {
  api.reconcileIdentities
    .mockResolvedValueOnce({
      rows: [
        { account_id: 7, status: 'failed', message: 'Repair authorization' },
        { account_id: 8, status: 'claimed' },
      ],
      next_after_id: 8,
      done: false,
    })
    .mockResolvedValueOnce({
      rows: [{ account_id: 9, status: 'claimed' }],
      next_after_id: 9,
      done: true,
    })
    .mockResolvedValueOnce({
      rows: [{ account_id: 7, status: 'claimed' }],
      next_after_id: 7,
      done: false,
    })
  const w = render()
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.reconcileIdentities).toHaveBeenCalledTimes(1)
  expect(api.reconcileIdentities).toHaveBeenLastCalledWith({
    after_id: 0,
    limit: 10,
  })
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.reconcileIdentities).toHaveBeenLastCalledWith({
    after_id: 8,
    limit: 10,
  })
  expect(w.findAll('tbody tr')).toHaveLength(3)
  expect(w.text()).toContain('Repair authorization')
  await w.find('tbody button').trigger('click')
  await flushPromises()
  expect(api.reconcileIdentities).toHaveBeenLastCalledWith({
    after_id: 6,
    limit: 1,
  })
  expect((w.find('input').element as HTMLInputElement).value).toBe('9')
  expect(w.findAll('tbody tr')).toHaveLength(3)
  expect(w.find('tbody button').exists()).toBe(false)
  w.unmount()
})
it('rejects invalid bounds and preserves the retry cursor after uncertain failure', async () => {
  const w = render()
  await w.findAll('input')[1].setValue(26)
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.reconcileIdentities).not.toHaveBeenCalled()
  expect(w.find('[role=alert]').text()).toBe(en.reconcileBounds)
  await w.findAll('input')[0].setValue(33)
  await w.findAll('input')[1].setValue(25)
  api.reconcileIdentities.mockRejectedValueOnce(
    new Error('Request interrupted'),
  )
  await w.find('form').trigger('submit')
  await flushPromises()
  expect((w.find('input').element as HTMLInputElement).value).toBe('33')
  expect(w.find('[role=alert]').text()).toContain('Request interrupted')
  expect(api.reconcileIdentities).toHaveBeenCalledTimes(1)
  w.unmount()
})
