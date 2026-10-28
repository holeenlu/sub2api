import { effectScope, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useBatchImageAccess } from '../queries/useBatchImageAccess'
import { createJobsTestServices } from './services'
import type { JobsServices, JobsPersonalKey } from '../services'
const eligible: JobsPersonalKey = {
  id: 1, key: 'public-api-key', name: 'image', status: 'active', group: {
    name: 'image group', platform: 'gemini', allow_batch_image_generation: true,
  }
}
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
describe('batch image access query ownership', () => {
  it('retries after failure and caches a completed empty result in this scope', async () => {
    const list = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [], pages: 1 })
    const scope = effectScope()
    const access = scope.run(() => useBatchImageAccess(createJobsTestServices({ listPersonalKeys: list })))
    if (!access) throw new Error('No active test scope')
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(access.batchImageAccessLoaded.value).toBe(false)
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(access.batchImageAccessLoaded.value).toBe(true)
    await access.refreshBatchImageAccess()
    expect(list).toHaveBeenCalledTimes(2)
    scope.stop()
  })
  it('aborts and rejects a late eligible result after identity changes', async () => {
    const pending = deferred<Awaited<ReturnType<JobsServices['listPersonalKeys']>>>()
    const scopeKey = ref<string | null>('user-a')
    const list = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue({ items: [], pages: 1 })
    const scope = effectScope()
    const access = scope.run(() => useBatchImageAccess(createJobsTestServices({ scopeKey, listPersonalKeys: list })))
    if (!access) throw new Error('No active test scope')
    const old = access.refreshBatchImageAccess()
    const signal = list.mock.calls[0][3].signal
    scopeKey.value = 'user-b'
    expect(signal.aborted).toBe(true)
    await access.refreshBatchImageAccess()
    pending.resolve({ items: [eligible], pages: 1 })
    expect(await old).toBe(false)
    expect(access.canUseBatchImage.value).toBe(false)
    expect(access.batchImageAccessLoading.value).toBe(false)
    scope.stop()
  })
  it('aborts unmounted work and never continues pagination after revocation', async () => {
    const pending = deferred<Awaited<ReturnType<JobsServices['listPersonalKeys']>>>()
    const list = vi.fn().mockReturnValue(pending.promise)
    const scope = effectScope()
    const access = scope.run(() => useBatchImageAccess(createJobsTestServices({ listPersonalKeys: list })))
    if (!access) throw new Error('No active test scope')
    const work = access.refreshBatchImageAccess()
    scope.stop()
    pending.resolve({ items: [], pages: 2 })
    expect(await work).toBe(false)
    expect(list.mock.calls[0][3].signal.aborted).toBe(true)
    expect(list).toHaveBeenCalledTimes(1)
    expect(access.batchImageAccessLoading.value).toBe(false)
  })
})
