import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PortalUpdate from '../components/PortalUpdate.vue'
import vendor from '@/i18n/locales/en/vendor'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ default: api }))
let state: Record<string, unknown>
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  state = { status: 'idle', portal_version: '0.2.17.12', backend_version: '0.2.17.13', mode: 'portal' }
  api.get.mockImplementation(async (url: string) => ({ data: url === '/portal-build.json'
    ? { version: '0.2.17.12' } : url === '/admin/system/version' ? { version: '0.2.17.13' } : { ...state } }))
  api.post.mockResolvedValue({ data: { status: 'running' } })
  wrapper = mount(PortalUpdate, { global: {
    plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { vendor } } })],
    stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } },
  } })
})
afterEach(() => { wrapper.unmount(); vi.useRealTimers() })
async function show() { await flushPromises(); await wrapper.get('button').trigger('click'); await flushPromises() }
function syncButton() { return wrapper.findAll('button').find(b => b.text().startsWith('Synchronize'))! }

describe('supplier portal updates', () => {
  it('shows the portal version separately and targets the running backend, not latest', async () => {
    await show()
    expect(wrapper.text()).toContain('0.2.17.12')
    expect(wrapper.text()).toContain('0.2.17.13')
    await syncButton().trigger('click')
    expect(api.post).toHaveBeenCalledWith('/portal-updater/update', { version: '0.2.17.13' })
    expect(syncButton().attributes('disabled')).toBeDefined()
  })
  it('does not report completion from backend version alone and verifies the actual portal image', async () => {
    await show()
    await syncButton().trigger('click')
    state.status = 'succeeded'
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).not.toContain('Reload the updated portal')
    state.portal_version = '0.2.17.13'
    await wrapper.findAll('button').find(b => b.text() === 'Check status')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Portal and backend versions match.')
  })
  it('disables updates when no host updater is connected', async () => {
    api.get.mockImplementation(async (url: string) => {
      if (url === '/portal-updater/status') throw new Error('502')
      return { data: { version: url === '/portal-build.json' ? '0.2.17.12' : '0.2.17.13' } }
    })
    await show()
    expect(wrapper.text()).toContain('Install the new host updater')
    expect(syncButton().attributes('disabled')).toBeDefined()
    expect(api.post).not.toHaveBeenCalled()
  })
  it('surfaces rollback failures and stops polling after unmount', async () => {
    await show()
    await syncButton().trigger('click')
    state.status = 'failed'
    state.message = 'previous image restored for all managed services'
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain(state.message)
    const calls = api.get.mock.calls.length
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.get).toHaveBeenCalledTimes(calls)
  })
})
