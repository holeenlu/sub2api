import { effectScope, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useOperationsRequest } from '../queries/useOperationsRequest'
describe('observation request ownership', () => {
  it('rejects old filters even if the transport ignores abort', async () => {
    const scope = effectScope()
    const owner = scope.run(() => useOperationsRequest({ scopeKey: ref('admin:scope') }))
    if (!owner) throw new Error('No active test scope')
    const first = owner.begin('events')
    const second = owner.begin('events')
    expect(first.signal.aborted).toBe(true)
    await expect(first.wait(Promise.resolve({ operation_id: 'old', actual_cost: null }))).rejects.toMatchObject({ name: 'AbortError' })
    expect(await second.wait(Promise.resolve({ operation_id: 'new', actual_cost: null }))).toEqual({ operation_id: 'new', actual_cost: null })
    scope.stop()
    expect(second.signal.aborted).toBe(true)
  })
  it('clears private projections synchronously on scope change and rejects return to an old scope', async () => {
    const scopeKey = ref<string | null>('a')
    const scope = effectScope()
    const owner = scope.run(() => useOperationsRequest({ scopeKey }))
    if (!owner) throw new Error('No active test scope')
    let content: string | null = 'sensitive prompt'
    owner.onScopeChange(() => { content = null })
    const first = owner.begin('detail')
    scopeKey.value = 'b'
    expect(content).toBeNull()
    scopeKey.value = 'a'
    expect(first.isCurrent()).toBe(false)
    scope.stop()
  })
})
