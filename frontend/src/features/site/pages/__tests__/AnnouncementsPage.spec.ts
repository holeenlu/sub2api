import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AnnouncementsPage from '../AnnouncementsPage.vue'
import { installAnnouncementTestServices } from '../../__tests__/announcementServices'
import type { Announcement } from '../../model/announcement'
const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() }))
vi.mock('../../api/adminAnnouncements', () => ({ adminAnnouncementsAPI: api }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/shared/lib/format', async original => ({ ...await original<typeof import('@/shared/lib/format')>(), formatDateTime: () => 'now' }))
const notice: Announcement = { id: 1, title: 'Notice', content: 'Body', status: 'active', notify_mode: 'popup', targeting: { any_of: [] }, created_at: '', updated_at: '' }
const response = (items: Announcement[]) => ({ items, total: items.length, pages: 1, page: 1, page_size: 20 })
const scopeKey = ref<string | null>('session-a:admin')
const canManage = ref(true)
let services: ReturnType<typeof installAnnouncementTestServices>
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  scopeKey.value = 'session-a:admin'; canManage.value = true
  services = installAnnouncementTestServices({ scopeKey, canManage })
  api.list.mockResolvedValue(response([notice]))
  api.update.mockResolvedValue(notice)
})
function page() {
  return mount(AnnouncementsPage, { global: { stubs: {
    TablePageLayout: { template: '<main><slot name="filters" /><slot name="table" /><slot name="pagination" /></main>' },
    DataTable: { props: ['data'], template: '<div><section v-for="row in data" :key="row.id"><span>{{ row.title }}</span><slot name="cell-actions" :row="row" /></section></div>' },
    BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
    ConfirmDialog: true, AnnouncementTargetingEditor: true, AnnouncementReadStatusDialog: true, AnnouncementPopup: true,
    Pagination: true, Select: true, Icon: true, EmptyState: true,
  } } })
}
describe('announcement admin composition', () => {
  it('preserves read-only admin actions while management controls require the port permission', async () => {
    canManage.value = false
    const wrapper = page()
    await flushPromises()
    expect(wrapper.find('[title="common.edit"]').exists()).toBe(false)
    expect(wrapper.find('[title="common.delete"]').exists()).toBe(false)
    expect(wrapper.find('[title="admin.announcements.readStatus"]').exists()).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.announcements.createAnnouncement')).toBe(false)
    expect(api.create).not.toHaveBeenCalled()
  })
  it('saves only changed fields and leaves the loaded announcement untouched', async () => {
    const wrapper = page()
    await flushPromises()
    await wrapper.get('[title="common.edit"]').trigger('click')
    await wrapper.get('#announcement-form textarea').setValue('Edited body')
    expect(notice.content).toBe('Body')
    await wrapper.get('#announcement-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(1, { content: 'Edited body' }, { signal: expect.any(AbortSignal) })
    expect(services.notifySuccess).toHaveBeenCalledWith('common.success')
  })
  it('discards a pending private list after the app changes scope', async () => {
    let resolve!: (value: ReturnType<typeof response>) => void
    api.list.mockReturnValueOnce(new Promise(res => { resolve = res }))
      .mockResolvedValueOnce(response([{ ...notice, id: 2, title: 'Current actor' }]))
    const wrapper = page()
    await flushPromises()
    const signal = api.list.mock.calls[0][3].signal as AbortSignal
    scopeKey.value = 'session-b:admin'
    await flushPromises()
    resolve(response([{ ...notice, title: 'Previous actor' }]))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.text()).toContain('Current actor')
    expect(wrapper.text()).not.toContain('Previous actor')
  })
  it('cancels editing and suppresses late save feedback after permission is withdrawn', async () => {
    let resolve!: (value: Announcement) => void
    api.update.mockReturnValueOnce(new Promise(res => { resolve = res }))
    const wrapper = page()
    await flushPromises()
    await wrapper.get('[title="common.edit"]').trigger('click')
    await wrapper.get('#announcement-form textarea').setValue('Edited body')
    await wrapper.get('#announcement-form').trigger('submit')
    const signal = api.update.mock.calls[0][2].signal as AbortSignal
    canManage.value = false
    await flushPromises()
    resolve(notice)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.find('#announcement-form').exists()).toBe(false)
    expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(services.notifyError).not.toHaveBeenCalled()
  })
})


describe('announcement command target changes', () => {
  it('does not close a newer delete target after an old cancelled delete resolves', async () => {
    let finish!: () => void
    api.delete.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }))
    api.list.mockResolvedValue(response([notice, { ...notice, id: 2, title: 'Next notice' }]))
    const wrapper = page()
    await flushPromises()
    await wrapper.findAll('[title="common.delete"]')[0].trigger('click')
    const dialog = wrapper.findComponent({ name: 'ConfirmDialog' })
    dialog.vm.$emit('confirm')
    await flushPromises()
    const signal = api.delete.mock.calls[0][1].signal as AbortSignal
    dialog.vm.$emit('cancel')
    await flushPromises()
    await wrapper.findAll('[title="common.delete"]')[1].trigger('click')
    finish()
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(dialog.props('show')).toBe(true)
    expect(services.notifySuccess).not.toHaveBeenCalled()
    expect(api.delete).toHaveBeenCalledTimes(1)
  })

  it('retains the draft after step-up cancellation without a failure toast or replay', async () => {
    api.update.mockRejectedValueOnce({ code: 'STEP_UP_CANCELLED', message: 'Cancelled' })
    const wrapper = page()
    await flushPromises()
    await wrapper.get('[title="common.edit"]').trigger('click')
    await wrapper.get('#announcement-form textarea').setValue('Unsaved draft')
    await wrapper.get('#announcement-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledTimes(1)
    expect(services.notifyError).not.toHaveBeenCalled()
    expect(services.notifySuccess).not.toHaveBeenCalled()
    expect((wrapper.get('#announcement-form textarea').element as HTMLTextAreaElement).value).toBe('Unsaved draft')
    expect(wrapper.get('button[form="announcement-form"]').attributes('disabled')).toBeUndefined()
  })
})
