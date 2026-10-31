import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { duplicate } from '../adminChannelMonitor'
import { useMonitorDuplicate } from '../../commands/useMonitorDuplicate'
import { createOperationsTestServices } from '../../__tests__/services'
const post = vi.hoisted(() => vi.fn())
vi.mock('@/shared/http', () => ({ apiClient: { post } }))
beforeEach(() => {
  sessionStorage.clear()
  post.mockReset().mockResolvedValue({ data: { id: 43, name: 'copy' } })
})
afterEach(() => vi.restoreAllMocks())
function owner(scopeKey = ref<string | null>('admin-7:session-a')) {
  const scope = effectScope()
  const command = scope.run(() => useMonitorDuplicate(createOperationsTestServices({ scopeKey })))
  if (!command) throw new Error('No command scope')
  return { scope, command, scopeKey }
}
describe('monitor duplicate command identity', () => {
  it('sends exactly the supplied operation key and signal', async () => {
    const signal = new AbortController().signal
    await duplicate(42, { idempotencyKey: 'stable-operation', signal })
    expect(post).toHaveBeenCalledWith('/admin/channel-monitors/42/duplicate', undefined,
      { signal, headers: { 'Idempotency-Key': 'stable-operation' } })
  })
  it('retains the same key on ambiguous failure and clears it on confirmed success', async () => {
    const { scope, command } = owner()
    post.mockRejectedValueOnce(new Error('timeout'))
    await expect(command.duplicateMonitor(99)).rejects.toThrow('timeout')
    expect(command.state.value.status).toBe('error')
    await command.duplicateMonitor(99)
    expect(post.mock.calls[1][2].headers).toEqual(post.mock.calls[0][2].headers)
    expect(sessionStorage.length).toBe(0)
    scope.stop()
  })
  it('recovers an ambiguous operation after remount in the same logical session', async () => {
    const first = owner()
    post.mockRejectedValueOnce(new Error('timeout'))
    await expect(first.command.duplicateMonitor(77)).rejects.toThrow('timeout')
    first.scope.stop()
    const second = owner()
    await second.command.duplicateMonitor(77)
    expect(post.mock.calls[1][2].headers).toEqual(post.mock.calls[0][2].headers)
    second.scope.stop()
  })
  it('never replays an old identity operation or accepts its late success', async () => {
    let finish: (value: unknown) => void = () => { throw new Error('No pending command') }
    post.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { scope, command, scopeKey } = owner()
    const old = command.duplicateMonitor(55)
    const oldSignal = post.mock.calls[0][2].signal
    scopeKey.value = 'admin-8:session-b'
    expect(oldSignal.aborted).toBe(true)
    expect(command.state.value.status).toBe('idle')
    await command.duplicateMonitor(55)
    finish({ data: { id: 99 } })
    await old
    expect(command.state.value).toMatchObject({ status: 'success', monitor: { id: 43 } })
    expect(post.mock.calls[1][2].headers).not.toEqual(post.mock.calls[0][2].headers)
    scope.stop()
  })
  it('does not issue commands without an effective scope', async () => {
    const { scope, command } = owner(ref(null))
    expect(await command.duplicateMonitor(66)).toBeUndefined()
    expect(post).not.toHaveBeenCalled()
    scope.stop()
  })
})
