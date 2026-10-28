import { beforeEach, describe, expect, it, vi } from 'vitest'
import { pluginsAPI as publicAPI } from '..'
import { pluginsAPI as apiEntry } from '../api'
import plugins, { pluginsAPI } from '../api/plugins'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

describe('system plugin API transport', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('publishes the same implementation through both public entries and the module default', () => {
    expect(publicAPI).toBe(pluginsAPI)
    expect(apiEntry).toBe(pluginsAPI)
    expect(plugins).toBe(pluginsAPI)
  })

  it('retains incompatible and unhealthy server state without substituting defaults', async () => {
    const records = [{ id: 7, state: 'incompatible', runtime_healthy: false, runtime_message: 'protocol mismatch' }]
    http.get.mockResolvedValue({ data: records })
    expect(await pluginsAPI.list()).toBe(records)
    expect(http.get).toHaveBeenCalledWith('/admin/plugins', undefined)
  })

  it('uploads the original archive with the existing multipart field and timeout', async () => {
    const file = new File(['fixture'], 'transport.s2plugin', { type: 'application/zip' })
    const result = { id: 7, state: 'disabled', signature_status: 'unsigned' }
    http.post.mockResolvedValue({ data: result })
    expect(await pluginsAPI.upload(file)).toBe(result)
    const [url, form, config] = http.post.mock.calls[0]
    expect(url).toBe('/admin/plugins/upload')
    expect(form).toBeInstanceOf(FormData)
    expect(form.getAll('plugin')).toEqual([file])
    expect(Array.from(form.keys())).toEqual(['plugin'])
    expect(config).toEqual({ headers: { 'Content-Type': 'multipart/form-data' }, timeout: 120000 })
  })

  it.each([[0, false], [100, true]])('preserves rollout %s and explicit accept_untested %s', async (rollout, accepted) => {
    const result = { id: 7, state: 'starting' }
    http.post.mockResolvedValue({ data: result })
    expect(await pluginsAPI.enable(7, Number(rollout), Boolean(accepted))).toBe(result)
    expect(http.post).toHaveBeenCalledWith('/admin/plugins/7/enable', { rollout_percent: rollout, accept_untested: accepted }, undefined)
  })

  it('keeps disable and removal as separate commands without inventing completion state', async () => {
    const disabled = { id: 7, state: 'disabled', last_error: 'previous failure' }
    http.post.mockResolvedValue({ data: disabled })
    expect(await pluginsAPI.disable(7)).toBe(disabled)
    expect(http.post).toHaveBeenCalledWith('/admin/plugins/7/disable', undefined, undefined)
    await expect(pluginsAPI.remove(7)).resolves.toBeUndefined()
    expect(http.delete).toHaveBeenCalledWith('/admin/plugins/7', undefined)
  })

  it('preserves opaque plugin configuration and uses the existing atomic PUT', async () => {
    const config = { enabled: false, token: '', nested: { limits: [0, null] } }
    http.get.mockResolvedValue({ data: config })
    http.put.mockResolvedValue({ data: config })
    expect(await pluginsAPI.getConfig(7)).toBe(config)
    expect(await pluginsAPI.saveConfig(7, config)).toBe(config)
    expect(http.get).toHaveBeenCalledWith('/admin/plugins/7/config', undefined)
    expect(http.put).toHaveBeenCalledWith('/admin/plugins/7/config', config, undefined)
    expect(http.put.mock.calls[0][1]).toBe(config)
  })

  it('preserves failed test, unhealthy status and UI bridge session facts', async () => {
    const testResult = { success: false, message: 'fixture rejection', latency_ms: 0, status_json: '{}' }
    const status = { healthy: false, message: 'starting', status_json: '{}' }
    const session = { url: '/plugin-ui/fixture', bridge_token: 'fixture-token', ui_bridge_version: 1, expires_at: '2026-10-25T12:00:00Z' }
    http.post.mockResolvedValueOnce({ data: testResult }).mockResolvedValueOnce({ data: session })
    http.get.mockResolvedValue({ data: status })
    expect(await pluginsAPI.test(7)).toBe(testResult)
    expect(await pluginsAPI.status(7)).toBe(status)
    expect(await pluginsAPI.createUISession(7)).toBe(session)
    expect(http.post.mock.calls).toEqual([['/admin/plugins/7/test', undefined, undefined], ['/admin/plugins/7/ui-session', undefined, undefined]])
    expect(http.get).toHaveBeenCalledWith('/admin/plugins/7/status', undefined)
  })

  it.each(['PERMISSION_DENIED', 'STEP_UP_REQUIRED', 'STEP_UP_ADMIN_API_KEY_FORBIDDEN', 'STEP_UP_NOT_ENABLED'])('propagates %s unchanged without retrying or bypassing the page step-up owner', async code => {
    const error = { response: { status: 403, data: { code } } }
    http.post.mockRejectedValue(error)
    await expect(pluginsAPI.enable(7, 0, false)).rejects.toBe(error)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.get).not.toHaveBeenCalled()
  })
})
