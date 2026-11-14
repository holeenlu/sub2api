import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AnnouncementPopup from '../AnnouncementPopup.vue'
import AnnouncementBell from '../AnnouncementBell.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import { useAnnouncementStore } from '@/features/site/queries/useAnnouncements'
import { installAnnouncementTestServices } from '@/features/site/__tests__/announcementServices'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/shared/lib/format', () => ({ formatRelativeTime: () => 'now', formatRelativeWithDateTime: () => 'now' }))
vi.mock('@/features/site/api/announcements', () => ({ announcementsAPI: { markRead: vi.fn().mockResolvedValue(undefined) } }))
installAnnouncementTestServices()
enableAutoUnmount(afterEach)

const announcement = {
  id: 1, title: 'Notice', content: 'Details', notify_mode: 'popup' as const,
  created_at: '2026-10-27', updated_at: '2026-10-27', read_at: '2026-10-27'
}
const global = { stubs: { Teleport: true, Transition: true, Icon: true } }
const popup = () => mount(AnnouncementPopup, { global })
const preview = () => mount(AnnouncementPopup, { props: { preview: true, announcement }, global })
const bell = () => mount(AnnouncementBell, { global })
const dialog = (show = true) => mount(BaseDialog, { props: { show, title: 'Editor' }, global })
const locked = () => {
  expect(document.body.style.overflow).toBe('hidden')
  expect(document.body.classList.contains('modal-open')).toBe(true)
}
const restored = () => {
  expect(document.body.style.overflow).toBe('scroll')
  expect(document.body.style.getPropertyPriority('overflow')).toBe('important')
  expect(document.body.classList.contains('modal-open')).toBe(false)
}

beforeEach(() => {
  setActivePinia(createPinia())
  useAnnouncementStore().setScope('session-a:user')
  useAnnouncementStore().announcements = [announcement]
  document.body.style.setProperty('overflow', 'scroll', 'important')
})
afterEach(() => {
  useAnnouncementStore().$dispose()
  document.body.removeAttribute('style')
  document.body.classList.remove('modal-open')
  vi.clearAllMocks()
})

describe('announcement and dialog scroll lock integration', () => {
  it('releases a normal popup on dismissal without a Bell on the page', async () => {
    useAnnouncementStore().currentPopup = announcement
    const wrapper = popup()
    locked()
    await wrapper.get('[data-testid="announcement-popup-dismiss"]').trigger('click')
    await flushPromises()
    expect(useAnnouncementStore().currentPopup).toBeNull()
    restored()
  })

  it('releases a normal popup on unmount even if its store still has an announcement', () => {
    useAnnouncementStore().currentPopup = announcement
    const wrapper = popup()
    locked()
    wrapper.unmount()
    expect(useAnnouncementStore().currentPopup?.id).toBe(announcement.id)
    restored()
  })

  it('does not let an inactive or unmounted Bell own the global popup lock', async () => {
    const wrapper = bell()
    useAnnouncementStore().currentPopup = announcement
    await nextTick()
    restored()
    const visible = popup()
    locked()
    wrapper.unmount()
    locked()
    visible.unmount()
    restored()
  })

  it.each(['popup-first', 'bell-first'] as const)('keeps the other overlay locked when closing %s', async order => {
    useAnnouncementStore().currentPopup = announcement
    const notice = popup()
    const list = bell()
    await list.get('button').trigger('click')
    locked()
    if (order === 'popup-first') {
      await notice.get('[data-testid="announcement-popup-dismiss"]').trigger('click')
      locked()
      await list.get('[aria-label="common.close"]').trigger('click')
    } else {
      await list.get('[aria-label="common.close"]').trigger('click')
      locked()
      await notice.get('[data-testid="announcement-popup-dismiss"]').trigger('click')
    }
    restored()
  })

  it('retains the list lock after closing details and releases on a scope change', async () => {
    const list = bell()
    await list.get('button').trigger('click')
    await list.get('.group.relative').trigger('click')
    locked()
    const closeButtons = list.findAll('[aria-label="common.close"]')
    expect(closeButtons).toHaveLength(2)
    await closeButtons[1].trigger('click')
    expect(list.find('.markdown-body').exists()).toBe(false)
    locked()
    useAnnouncementStore().setScope('session-b:user')
    await nextTick()
    restored()
  })

  it('keeps details locked if the underlying list closes first', async () => {
    const list = bell()
    await list.get('button').trigger('click')
    await list.get('.group.relative').trigger('click')
    await list.findAll('[aria-label="common.close"]')[0].trigger('click')
    expect(list.find('.markdown-body').exists()).toBe(true)
    locked()
    await list.get('[aria-label="common.close"]').trigger('click')
    restored()
  })

  it('releases Bell on unmount while preserving a separate preview lock', async () => {
    const list = bell()
    await list.get('button').trigger('click')
    const notice = preview()
    locked()
    list.unmount()
    locked()
    notice.unmount()
    restored()
  })

  it.each(['dialog-first', 'preview-first'] as const)('shares the lock with BaseDialog when closing %s', async order => {
    const editor = dialog()
    const notice = preview()
    dialog(false).unmount()
    locked()
    if (order === 'dialog-first') {
      await editor.setProps({ show: false })
      locked()
      await notice.setProps({ announcement: null })
    } else {
      await notice.setProps({ announcement: null })
      locked()
      await editor.setProps({ show: false })
    }
    restored()
  })

  it('keeps BaseDialog locked when a normal popup and an open Bell unmount', async () => {
    useAnnouncementStore().currentPopup = announcement
    const notice = popup()
    const list = bell()
    await list.get('button').trigger('click')
    const editor = dialog()
    notice.unmount()
    locked()
    list.unmount()
    locked()
    editor.unmount()
    restored()
  })

  it('does not relock an unmounted popup after pending reactive work', async () => {
    const notice = popup()
    useAnnouncementStore().currentPopup = announcement
    notice.unmount()
    await nextTick()
    restored()
  })
})
