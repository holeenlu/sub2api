import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTelemetryPanel from '../AccountTelemetryPanel.vue'
import { readAccountTelemetry, type AccountTelemetryState } from '@/utils/accountTelemetry'

const mocks = vi.hoisted(() => ({ import: vi.fn(), copy: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ importAccountTelemetry: mocks.import }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: mocks.copy }) }))

const bundle = {
  format: 'sub2api-account-telemetry', version: 2,
  exported_at: '2026-10-20T01:00:00Z', device_id: 'a'.repeat(64), machine_id: 'local-machine',
  cli_version: '2.1.304', platform: 'darwin', arch: 'arm64', locale: 'zh-CN',
  environment: { shell: 'zsh', cpu_cores: 8 }
}
const imported: AccountTelemetryState = {
  status: 'ready', revision: 'r1', owner: 'owner1', device_installation: bundle,
  imported_at: '2026-10-20T02:00:00Z', session_id: 'gateway-session'
}
const waiting: AccountTelemetryState = { status: 'waiting_import', revision: 'r0', owner: 'owner1' }

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function selectFile(wrapper: ReturnType<typeof mount>, file: { size: number; text?: () => Promise<string> }) {
  const input = wrapper.get('[data-testid="account-telemetry-import-file"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  return input.trigger('change')
}

const validFile = () => ({ size: 512, text: async () => JSON.stringify(bundle) })

describe('AccountTelemetryPanel', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.resetAllMocks() })
  afterEach(() => { vi.useRealTimers() })

  it('explains import after creation and offers no generation or download', () => {
    const wrapper = mount(AccountTelemetryPanel)
    expect(wrapper.text()).toContain('admin.accounts.telemetry.createHint')
    expect(wrapper.findAll('button, input, a')).toHaveLength(0)
    wrapper.unmount()
  })

  it.each(['pending', 'error', 'ready'])('shows old server-only %s data as waiting for import without polling', async (status) => {
    const state = readAccountTelemetry({ account_telemetry: {
      status, revision: 'old', owner: 'old-owner', generated_at: 'old-time', error_code: 'old-error',
      profile: { source: 'official-cc-init', payload_json: '{"private":"old"}', headers: { Authorization: ['secret'] } }
    } })
    expect(state).toEqual({ status: 'waiting_import', revision: 'old', owner: 'old-owner' })
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: state } })
    expect(wrapper.get('[data-testid="account-telemetry-status"]').text()).toContain('waiting_import')
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.findAll('a, textarea')).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(10000)
    expect(vi.getTimerCount()).toBe(0)
    expect(mocks.import).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves a saved v2 device bundle and copies only imported device facts', async () => {
    const state = readAccountTelemetry({ account_telemetry: {
      ...imported, status: 'error', generated_at: 'old-generated-time', profile: { payload_json: 'old-event', headers: {} }
    } })
    expect(state).toEqual(imported)
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: state } })
    expect(wrapper.get('[data-testid="account-telemetry-deviceId"]').text()).toContain(bundle.device_id)
    expect(wrapper.get('[data-testid="account-telemetry-cliVersion"]').text()).toContain(bundle.cli_version)
    expect(wrapper.get('[data-testid="account-telemetry-sessionId"]').text()).toContain(imported.session_id)
    expect(wrapper.get('[data-testid="account-telemetry-importedAt"]').text()).toContain(imported.imported_at)
    expect(wrapper.get('[data-testid="account-telemetry-import"]').text()).toContain('reimport')
    const preview = wrapper.get('textarea')
    expect(preview.attributes('readonly')).toBeDefined()
    expect(JSON.parse((preview.element as HTMLTextAreaElement).value)).toEqual(bundle)
    await wrapper.get('[data-testid="account-telemetry-copy"]').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith(JSON.stringify(bundle, null, 2))
    wrapper.unmount()
  })

  it('treats missing or malformed device data as waiting for import', () => {
    expect(readAccountTelemetry(null)).toBeNull()
    expect(readAccountTelemetry({ account_telemetry: [] })).toBeNull()
    for (const device of [undefined, { ...bundle, version: 1 }, { ...bundle, environment: [] }, { ...bundle, device_id: null }]) {
      const state = readAccountTelemetry({ account_telemetry: { ...imported, device_installation: device } })
      expect(state?.status).toBe('waiting_import')
      expect(state?.device_installation).toBeUndefined()
    }
  })

  it('imports once while busy and keeps the result across equivalent parent renders', async () => {
    const request = deferred<AccountTelemetryState>()
    mocks.import.mockReturnValue(request.promise)
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: waiting } })
    await selectFile(wrapper, validFile()); await flushPromises()
    expect(mocks.import).toHaveBeenCalledWith(42, bundle)
    await wrapper.setProps({ value: { ...waiting } })
    expect(wrapper.get('[data-testid="account-telemetry-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.attributes('aria-busy')).toBe('true')
    await selectFile(wrapper, validFile()); await flushPromises()
    expect(mocks.import).toHaveBeenCalledTimes(1)
    request.resolve(imported); await flushPromises()
    expect(wrapper.get('[data-testid="account-telemetry-status"]').text()).toContain('ready')
    expect(wrapper.get('[data-testid="account-telemetry-import"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ value: { ...waiting } })
    expect(wrapper.get('[data-testid="account-telemetry-deviceId"]').text()).toContain(bundle.device_id)
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

  it('rejects oversized and malformed files before upload and keeps the imported data', async () => {
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: imported } })
    const text = vi.fn()
    await selectFile(wrapper, { size: 1024 * 1024 + 1, text }); await flushPromises()
    expect(text).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('importTooLarge')
    await selectFile(wrapper, { size: 2, text: async () => '{' }); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('importInvalid')
    expect(mocks.import).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="account-telemetry-deviceId"]').text()).toContain(bundle.device_id)
    wrapper.unmount()
  })

  it.each([
    ['telemetry_import_invalid', 'importInvalid'],
    ['telemetry_account_changed', 'importAccountChanged'],
    [null, 'importFailed']
  ])('reports upload failure %s without replacing the imported bundle', async (message, key) => {
    mocks.import.mockRejectedValue(message ? { message } : {})
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: imported } })
    await selectFile(wrapper, validFile()); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(key)
    expect(wrapper.get('[data-testid="account-telemetry-status"]').text()).toContain('ready')
    expect(wrapper.get('[data-testid="account-telemetry-import"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not upload a file after switching accounts during the read', async () => {
    const read = deferred<string>()
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42 } })
    await selectFile(wrapper, { size: 512, text: () => read.promise })
    await wrapper.setProps({ accountId: 43 })
    read.resolve(JSON.stringify(bundle)); await flushPromises()
    expect(mocks.import).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="account-telemetry-import"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('ignores an old upload response and preserves the next account’s busy state', async () => {
    const oldRequest = deferred<AccountTelemetryState>()
    const nextRequest = deferred<AccountTelemetryState>()
    mocks.import.mockReturnValueOnce(oldRequest.promise).mockReturnValueOnce(nextRequest.promise)
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42 } })
    await selectFile(wrapper, validFile()); await flushPromises()
    await wrapper.setProps({ accountId: 43 })
    await selectFile(wrapper, validFile()); await flushPromises()
    oldRequest.resolve(imported); await flushPromises()
    expect(wrapper.get('[data-testid="account-telemetry-status"]').text()).toContain('waiting_import')
    expect(wrapper.get('[data-testid="account-telemetry-import"]').attributes('disabled')).toBeDefined()
    nextRequest.resolve({ ...imported, session_id: 'second-account-session' }); await flushPromises()
    expect(wrapper.get('[data-testid="account-telemetry-sessionId"]').text()).toContain('second-account-session')
    wrapper.unmount()
  })

  it('invalidates an old upload when the same account gets a new revision', async () => {
    const request = deferred<AccountTelemetryState>()
    mocks.import.mockReturnValue(request.promise)
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42, value: waiting } })
    await selectFile(wrapper, validFile()); await flushPromises()
    await wrapper.setProps({ value: { ...imported, revision: 'newer', session_id: 'newer-session' } })
    request.resolve(imported); await flushPromises()
    expect(wrapper.get('[data-testid="account-telemetry-sessionId"]').text()).toContain('newer-session')
    wrapper.unmount()
  })

  it('does not upload after the panel is closed while reading a file', async () => {
    const read = deferred<string>()
    const wrapper = mount(AccountTelemetryPanel, { props: { accountId: 42 } })
    await selectFile(wrapper, { size: 512, text: () => read.promise })
    wrapper.unmount()
    read.resolve(JSON.stringify(bundle)); await flushPromises()
    expect(mocks.import).not.toHaveBeenCalled()
  })
})
