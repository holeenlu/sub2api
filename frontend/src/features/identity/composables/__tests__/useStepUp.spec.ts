import { effectScope } from 'vue'
import { authSessionStamp } from '@/shared/session'
import { describe, it, expect, vi } from 'vitest'
import {
  useStepUp,
  globalAdminStepUp, requestAdminStepUp,
  isStepUpRequired,
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  StepUpCancelledError
} from '../useStepUp'

describe('useStepUp error classification', () => {
  it('detects STEP_UP_REQUIRED from code field', () => {
    expect(isStepUpRequired({ status: 403, code: 'STEP_UP_REQUIRED' })).toBe(true)
    expect(isStepUpRequired({ status: 403, reason: 'STEP_UP_REQUIRED' })).toBe(true)
    expect(isStepUpRequired({ status: 500, code: 'INTERNAL' })).toBe(false)
    expect(isStepUpRequired(null)).toBe(false)
  })

  it('detects blocked (non-retryable) step-up errors', () => {
    expect(isStepUpBlocked({ code: 'STEP_UP_TOTP_NOT_ENABLED' })).toBe(true)
    expect(isStepUpBlocked({ reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' })).toBe(true)
    expect(isStepUpBlocked({ code: 'STEP_UP_REQUIRED' })).toBe(false)
  })

  it('surfaces the block reason marker', () => {
    expect(stepUpBlockReason({ reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' })).toBe('STEP_UP_ADMIN_API_KEY_FORBIDDEN')
    expect(stepUpBlockReason({ code: 'OTHER' })).toBe('')
  })
})

describe('useStepUp concurrent prompts', () => {
  it.each([true, false])('settles every waiter when verification resolves to %s', async (verified) => {
    const stepUp = useStepUp()
    const firstDone = vi.fn()
    const secondDone = vi.fn()
    const first = stepUp.prompt()
    first.then(firstDone)
    stepUp.prompt().then(secondDone)
    if (verified) stepUp.onVerified()
    else stepUp.onCancel()
    await Promise.resolve()
    expect(firstDone).toHaveBeenCalledWith(verified)
    expect(secondDone).toHaveBeenCalledWith(verified)
    expect(stepUp.visible.value).toBe(false)
    const next = stepUp.prompt()
    expect(next).not.toBe(first)
    stepUp.onCancel()
    await expect(next).resolves.toBe(false)
  })
})

describe('useStepUp.run', () => {
  it('returns the action result directly on success', async () => {
    const stepUp = useStepUp()
    const result = await stepUp.run(async () => 42)
    expect(result).toBe(42)
    expect(stepUp.visible.value).toBe(false)
  })

  it('does not replay a verified action under a replacement login', async () => {
    localStorage.setItem('auth_token', 'first-session')
    const stepUp = useStepUp()
    const action = vi.fn().mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' }).mockResolvedValueOnce('wrong-user-action')
    const pending = stepUp.run(action)
    const rejected = expect(pending).rejects.toBeInstanceOf(StepUpCancelledError)
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    localStorage.setItem('auth_token', 'second-session')
    stepUp.onVerified()
    await rejected
    expect(action).toHaveBeenCalledTimes(1)
    localStorage.clear()
  })

  it('re-throws non-step-up errors without prompting', async () => {
    const stepUp = useStepUp()
    const err = { status: 500, code: 'INTERNAL' }
    await expect(stepUp.run(async () => { throw err })).rejects.toBe(err)
    expect(stepUp.visible.value).toBe(false)
  })

  it('re-throws blocked errors without prompting', async () => {
    const stepUp = useStepUp()
    const err = { status: 403, code: 'STEP_UP_TOTP_NOT_ENABLED' }
    await expect(stepUp.run(async () => { throw err })).rejects.toBe(err)
    expect(stepUp.visible.value).toBe(false)
  })

  it('prompts on STEP_UP_REQUIRED and retries after verification', async () => {
    const stepUp = useStepUp()
    let calls = 0
    const action = async () => {
      calls++
      if (calls === 1) throw { status: 403, code: 'STEP_UP_REQUIRED' }
      return 'ok'
    }
    const promise = stepUp.run(action)
    // The dialog should now be open, awaiting verification (after the first rejection is handled).
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    stepUp.onVerified()
    await expect(promise).resolves.toBe('ok')
    expect(calls).toBe(2)
    expect(stepUp.visible.value).toBe(false)
  })

  it('throws a cancellation sentinel (not the original error) if the user cancels the prompt', async () => {
    const stepUp = useStepUp()
    const err = { status: 403, code: 'STEP_UP_REQUIRED' }
    const promise = stepUp.run(async () => { throw err })
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    stepUp.onCancel()
    await expect(promise).rejects.toBeInstanceOf(StepUpCancelledError)
    expect(stepUp.visible.value).toBe(false)
  })

  it('classifies the cancellation sentinel distinctly from step-up errors', () => {
    const cancelled = new StepUpCancelledError()
    expect(isStepUpCancelled(cancelled)).toBe(true)
    expect(isStepUpRequired(cancelled)).toBe(false)
    expect(isStepUpBlocked(cancelled)).toBe(false)
    expect(isStepUpCancelled({ status: 403, code: 'STEP_UP_REQUIRED' })).toBe(false)
  })
})

describe('shared administrative verification', () => {
  it('shares a prompt and cancels all waiting actions together', async () => {
    const first = requestAdminStepUp()
    const second = requestAdminStepUp()
    expect(first).toBe(second)
    globalAdminStepUp.onCancel()
    expect(await first).toBe(false)
    expect(await second).toBe(false)
  })
  it('permits token refresh in the same family but separates a different login or credential generation', () => {
    const token = (sid: string, generation: number, exp: number) => `e30.${btoa(JSON.stringify({user_id: 1, role: 'admin', sid, token_version: generation, exp}))}.signature`
    expect(authSessionStamp(token('a', 1, 5))).toBe(authSessionStamp(token('a', 1, 9)))
    expect(authSessionStamp(token('a', 1, 5))).not.toBe(authSessionStamp(token('b', 1, 5)))
    expect(authSessionStamp(token('a', 1, 5))).not.toBe(authSessionStamp(token('a', 2, 5)))
  })
})


describe('step-up lifecycle', () => {
  it('does not reopen verification when a required response arrives after disposal', async () => {
    let reject!: (error: unknown) => void
    const scope = effectScope()
    const controller = scope.run(() => useStepUp())!
    const action = vi.fn(() => new Promise<void>((_resolve, fail) => { reject = fail }))
    const outcome = controller.run(action).catch(error => error)
    scope.stop()
    reject({ code: 'STEP_UP_REQUIRED' })
    await Promise.resolve()
    await Promise.resolve()
    try {
      expect(controller.visible.value).toBe(false)
    } finally { controller.onCancel() }
    expect(await outcome).toBeInstanceOf(StepUpCancelledError)
    expect(action).toHaveBeenCalledOnce()
  })

  it('does not start an action or a prompt after its owner is disposed', async () => {
    const scope = effectScope()
    const controller = scope.run(() => useStepUp())!
    const action = vi.fn().mockResolvedValue('secret')
    scope.stop()
    await expect(controller.run(action)).rejects.toBeInstanceOf(StepUpCancelledError)
    expect(action).not.toHaveBeenCalled()
    await expect(controller.prompt()).resolves.toBe(false)
  })

  it.each(['dispose', 'session', 'cancel'] as const)('rejects a successful late action after %s', async boundary => {
    localStorage.setItem('auth_token', 'first-session')
    let resolve!: (value: string) => void
    const scope = effectScope()
    const controller = scope.run(() => useStepUp())!
    const pending = controller.run(() => new Promise<string>(done => { resolve = done }))
    const result = pending.catch(error => error)
    if (boundary === 'dispose') scope.stop()
    if (boundary === 'session') localStorage.setItem('auth_token', 'replacement-session')
    if (boundary === 'cancel') controller.onCancel()
    resolve('obsolete-secret')
    try { expect(await result).toBeInstanceOf(StepUpCancelledError) }
    finally { scope.stop(); localStorage.clear() }
  })

  it('settles a pending local prompt when its component scope is disposed', async () => {
    const scope = effectScope()
    const controller = scope.run(() => useStepUp())!
    const pending = controller.prompt()
    scope.stop()
    await expect(pending).resolves.toBe(false)
    expect(controller.visible.value).toBe(false)
  })

  it('does not share verification across a replacement session', async () => {
    globalAdminStepUp.onCancel()
    localStorage.setItem('auth_token', 'old-login')
    const old = requestAdminStepUp()
    localStorage.setItem('auth_token', 'new-login')
    const current = requestAdminStepUp()
    expect(current).not.toBe(old)
    await expect(old).resolves.toBe(false)
    expect(globalAdminStepUp.visible.value).toBe(true)
    globalAdminStepUp.onVerified()
    await expect(current).resolves.toBe(true)
    localStorage.clear()
  })
})
