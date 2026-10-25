import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, type EffectScope } from 'vue'
import { usePortalVersion } from '../version'

const mocks = vi.hoisted(() => ({ get: vi.fn(), backend: vi.fn() }))
vi.mock('@/api/client', () => ({ default: { get: mocks.get } }))
vi.mock('@/api/admin/system', () => ({ getVersion: mocks.backend }))
const scopes: EffectScope[] = []

function createSource() {
  const scope = effectScope()
  scopes.push(scope)
  const source = scope.run(usePortalVersion)
  if (!source) throw new Error('Expected an active effect scope')
  return { source, scope }
}

function deferred<T>() {
  let resolve = (_value: T): void => { throw new Error('Promise is not initialized') }
  const promise = new Promise<T>((accept) => { resolve = accept })
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ data: { version: '1.0.0', commit: 'abcdef0' } })
  mocks.backend.mockResolvedValue({ version: '1.1.0', upstream_version: '0.9.0', build_commit: '1234567' })
})
afterEach(() => { scopes.splice(0).forEach(scope => scope.stop()) })

describe('portal metadata lifecycle', () => {
  it('coalesces concurrent reads and refreshes only when explicitly requested', async () => {
    const { source } = createSource()
    const first = source.fetchVersion()
    const concurrent = source.fetchVersion()
    expect(concurrent).toBe(first)
    expect(await first).toBe(true)
    await source.fetchVersion()
    expect(mocks.get).toHaveBeenCalledOnce()
    expect(mocks.backend).toHaveBeenCalledOnce()
    await source.fetchVersion(true)
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.backend).toHaveBeenCalledTimes(2)
  })

  it('clears stale facts for the failing source without cross-filling from the other source', async () => {
    const { source } = createSource()
    await source.fetchVersion()
    mocks.get.mockRejectedValueOnce(new Error('not found'))
    expect(await source.fetchVersion(true)).toBe(false)
    expect(source.currentVersion).toBe('')
    expect(source.buildCommit).toBe('')
    expect(source.backendVersion).toBe('1.1.0')
  })

  it('does not apply a late response after the source scope is disposed', async () => {
    const pending = deferred<{ data: { version: string } }>()
    mocks.get.mockReturnValueOnce(pending.promise)
    const { source, scope } = createSource()
    const request = source.fetchVersion()
    scope.stop()
    pending.resolve({ data: { version: 'old' } })
    expect(await request).toBe(false)
    expect(source.currentVersion).toBe('')
    expect(source.backendVersion).toBe('')
    expect(source.versionLoaded).toBe(false)
    expect(source.versionLoading).toBe(false)
  })

  it('does not let an invalidated response overwrite a new read', async () => {
    const pending = deferred<{ data: { version: string } }>()
    mocks.get.mockReturnValueOnce(pending.promise)
    const { source } = createSource()
    const oldRequest = source.fetchVersion()
    source.clearVersionCache()
    mocks.get.mockResolvedValueOnce({ data: { version: '2.0.0' } })
    await source.fetchVersion()
    pending.resolve({ data: { version: '1.0.0' } })
    await oldRequest
    expect(source.currentVersion).toBe('2.0.0')
    expect(source.versionLoaded).toBe(true)
    expect(source.versionLoading).toBe(false)
  })
})
