import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import VersionBadge from '../VersionBadge.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), backend: vi.fn(), copy: vi.fn(), app: vi.fn() }))
const auth = reactive({ isSuperAdmin: true })
vi.mock('@/api/client', () => ({ default: { get: mocks.get } }))
vi.mock('@/api/admin/system', () => ({ getVersion: mocks.backend }))
vi.mock('@/stores', () => ({ useAuthStore: () => auth, useAppStore: mocks.app }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  auth.isSuperAdmin = true
  mocks.get.mockResolvedValue({ data: { version: '1.2.3', commit: 'a'.repeat(40) } })
  mocks.backend.mockResolvedValue({ version: '1.2.4', upstream_version: '1.2.0', build_commit: 'b'.repeat(40) })
  mocks.copy.mockResolvedValue(true)
})

async function open() {
  const wrapper = mount(VersionBadge, { props: { portal: true, version: '9.9.9' }, global: { stubs: { Icon: true } } })
  await flushPromises()
  await wrapper.get('button').trigger('click')
  return wrapper
}

describe('read-only portal and backend versions', () => {
  it('reads the actual portal manifest and backend metadata separately', async () => {
    const wrapper = await open()
    expect(mocks.get.mock.calls).toEqual([['/portal-build.json', { baseURL: '/' }]])
    expect(mocks.backend).toHaveBeenCalledOnce()
    expect(mocks.app).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('v1.2.3')
    expect(wrapper.get('[data-testid="backend-version"]').text()).toBe('v1.2.4')
    expect(wrapper.get('[data-testid="build-commit"]').attributes('title')).toBe('a'.repeat(40))
    expect(wrapper.get('[data-testid="backend-build-commit"]').attributes('title')).toBe('b'.repeat(40))
    const copy = wrapper.findAll('button').find(button => button.text() === 'version.copyDetails')
    await copy?.trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith(expect.stringContaining('vendor.portalVersion.portalVersion: v1.2.3\nvendor.portalVersion.backendVersion: v1.2.4'))
    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['v1.2.3', 'version.refresh', 'version.copyDetails'])
  })

  it.each([{ version: '' }, { version: 'unknown' }, { version: 123 }, '<html>SPA fallback</html>'])('does not replace an unknown portal build with the backend or public prop (%j)', async (data) => {
    mocks.get.mockResolvedValue({ data })
    const wrapper = await open()
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="backend-version"]').text()).toBe('v1.2.4')
    expect(wrapper.get('[role="status"]').text()).toBe('version.unavailable')
    expect(wrapper.text()).not.toContain('9.9.9')
  })

  it('retains the real portal version when backend metadata is unavailable', async () => {
    mocks.backend.mockRejectedValue(new Error('403'))
    const wrapper = await open()
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('v1.2.3')
    expect(wrapper.get('[data-testid="backend-version"]').text()).toBe('—')
    expect(wrapper.find('[data-testid="backend-build-commit"]').exists()).toBe(false)
  })

  it('does not fetch either endpoint for a non-super-admin', async () => {
    auth.isSuperAdmin = false
    const wrapper = mount(VersionBadge, { props: { portal: true } })
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(mocks.backend).not.toHaveBeenCalled()
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
