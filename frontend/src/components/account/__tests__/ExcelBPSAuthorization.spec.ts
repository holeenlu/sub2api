import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ExcelBPSAuthorization from '../ExcelBPSAuthorization.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => { vi.resetAllMocks() })

describe('Excel BPS authorization isolation', () => {
  it('does not let a stale account status overwrite the newly selected account', async () => {
    let resolveOld!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    api.get.mockResolvedValueOnce({ data: { authorized: false } })
    const wrapper = mount(ExcelBPSAuthorization, { props: { accountId: 1 } })
    await wrapper.setProps({ accountId: 2 })
    await flushPromises()
    resolveOld({ data: { authorized: true } })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('authorization.required')
    expect(api.get).toHaveBeenLastCalledWith('/admin/openai/accounts/2/excel-authorization')
    wrapper.unmount()
  })

  it('manages primary Excel credentials through the existing account OAuth UI', async () => {
    api.get.mockResolvedValue({ data: { authorized: true, source: 'primary' } })
    const wrapper = mount(ExcelBPSAuthorization, { props: { accountId: 1 } })
    await flushPromises()
    expect(wrapper.text()).toContain('authorization.primary')
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('authorization.revoke')
    wrapper.unmount()
  })

  it('clears unsaved credential material when the account changes', async () => {
    api.get.mockResolvedValue({ data: { authorized: false } })
    const wrapper = mount(ExcelBPSAuthorization, { props: { accountId: 1 } })
    await flushPromises()
    await wrapper.get('textarea').setValue('{"refresh_token":"secret"}')
    await wrapper.setProps({ accountId: 2 })
    await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(api.post).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
