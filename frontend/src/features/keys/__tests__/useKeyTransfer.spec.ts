import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useKeyTransfer } from '../queries/useKeyTransfer'
import { keysServicesKey } from '../services'
import { createKeysTestServices } from './services'
const { exportData, importData, saveAs } = vi.hoisted(() => ({ exportData: vi.fn(), importData: vi.fn(), saveAs: vi.fn() }))
vi.mock('../api/keys', () => ({ keysAPI: { exportData, importData } }))
vi.mock('file-saver', () => ({ saveAs }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const backup = { type: 'sub2api-keys' as const, version: 1 as const, keys: [{ key: 'sk-private', status: 'disabled' }] }
function host() {
  const scopeKey = ref<string | null>('session:1')
  const services = createKeysTestServices({ scopeKey })
  const imported = vi.fn()
  const results: ReturnType<typeof useKeyTransfer>[] = []
  const wrapper = mount(defineComponent({ setup() {
    results.push(useKeyTransfer({ selectedIds: [1], filters: { group_id: 0 } }, imported))
    return () => null
  } }), { global: { provide: { [keysServicesKey as symbol]: services } } })
  return { wrapper, state: results[0], scopeKey, services, imported }
}
beforeEach(() => { exportData.mockReset(); importData.mockReset(); saveAs.mockReset() })
describe('Key transfer secret lifetime', () => {
  it('never downloads a late export after the session changes', async () => {
    let finish: (data: unknown) => void = () => undefined
    exportData.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const { wrapper, state, scopeKey, services } = host()
    state.showExport.value = true
    const pending = state.exportKeys()
    scopeKey.value = 'session:2'
    expect(exportData.mock.calls[0][2].signal.aborted).toBe(true)
    finish(backup); await pending
    expect(saveAs).not.toHaveBeenCalled(); expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(state.exporting.value).toBe(false); wrapper.unmount()
  })
  it('drops the old private file if its text finishes reading after logout', async () => {
    let finish: (text: string) => void = () => undefined
    const file = new File(['private'], 'keys.json')
    Object.defineProperty(file, 'text', { value: () => new Promise<string>(resolve => { finish = resolve }) })
    const input = document.createElement('input'); Object.defineProperty(input, 'files', { value: [file] })
    const event = new Event('change'); Object.defineProperty(event, 'target', { value: input })
    const { wrapper, state, scopeKey } = host()
    const pending = state.readFile(event); scopeKey.value = null
    finish(JSON.stringify(backup)); await pending
    expect(state.data.value).toBeNull(); expect(state.fileName.value).toBe(''); expect(state.reading.value).toBe(false)
    wrapper.unmount()
  })
  it('aborts the local import wait on disposal without reporting a late server success', async () => {
    let finish: (data: unknown) => void = () => undefined
    importData.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const { wrapper, state, imported } = host()
    state.data.value = backup
    const pending = state.importKeys()
    wrapper.unmount(); expect(importData.mock.calls[0][1].signal.aborted).toBe(true)
    finish({ created: 1, skipped: 0, failed: 0, errors: [] }); await pending; await flushPromises()
    expect(imported).not.toHaveBeenCalled(); expect(state.data.value).toBeNull(); expect(state.result.value).toBeNull()
  })
})
