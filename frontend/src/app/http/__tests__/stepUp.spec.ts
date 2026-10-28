import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { globalAdminStepUp } from '@/features/identity'
import { updateSettings } from '@/features/system/api/settings'
import { installApplicationHttp } from '../install'

vi.mock('@/i18n', () => ({ getLocale: () => 'en', i18n: { global: { t: (key: string) => key } } }))
const originalAdapter = apiClient.defaults.adapter
let dispose = () => {}
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_token', 'same-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 1 }))
  globalAdminStepUp.onCancel()
  dispose = installApplicationHttp()
})
afterEach(() => {
  globalAdminStepUp.onCancel()
  dispose()
  apiClient.defaults.adapter = originalAdapter
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('application step-up request ownership', () => {
  it('cancels one wait immediately without cancelling a second owner sharing verification', async () => {
    const first = new AbortController(), second = new AbortController()
    const removeListener = vi.spyOn(first.signal, 'removeEventListener')
    const attempts: string[] = []
    apiClient.defaults.adapter = async config => {
      const data = JSON.parse(config.data)
      attempts.push(data.site_name)
      if (attempts.filter(name => name === data.site_name).length === 1) {
        throw { config, response: { status: 403, data: { code: 'STEP_UP_REQUIRED' } } }
      }
      return { config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} }
    }
    const cancelled = updateSettings({ revision: 'a'.repeat(64), site_name: 'first' }, { signal: first.signal }).catch(error => error)
    const current = updateSettings({ revision: 'a'.repeat(64), site_name: 'second' }, { signal: second.signal })
    await vi.waitFor(() => {
      expect(attempts).toEqual(['first', 'second'])
      expect(globalAdminStepUp.visible.value).toBe(true)
    })
    first.abort()
    // Must settle while verification is still pending, not merely discard a later result.
    expect(await cancelled).toMatchObject({ code: 'ERR_CANCELED' })
    expect(removeListener).toHaveBeenCalledWith('abort', expect.any(Function))
    expect(globalAdminStepUp.visible.value).toBe(true)
    globalAdminStepUp.onVerified()
    expect(await current).toMatchObject({ site_name: 'second' })
    expect(attempts).toEqual(['first', 'second', 'second'])
  })

  it.each(['session', 'policy'] as const)('rechecks %s after verification before replaying', async change => {
    const adapter = vi.fn(async config => { throw { config, response: { status: 403, data: { code: 'STEP_UP_REQUIRED' } } } })
    apiClient.defaults.adapter = adapter
    const pending = updateSettings({ revision: 'a'.repeat(64), site_name: 'Mine' }, { signal: new AbortController().signal }).catch(error => error)
    await vi.waitFor(() => expect(globalAdminStepUp.visible.value).toBe(true))
    if (change === 'session') localStorage.setItem('auth_token', 'new-session')
    else localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 2 }))
    globalAdminStepUp.onVerified()
    expect(await pending).toMatchObject({ code: 'ERR_CANCELED' })
    expect(adapter).toHaveBeenCalledTimes(1)
  })
})
