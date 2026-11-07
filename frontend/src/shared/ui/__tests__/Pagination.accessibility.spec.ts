import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import zhTW from '@/i18n/locales/zh-TW'
import ja from '@/i18n/locales/ja'
import Pagination from '../Pagination.vue'

enableAutoUnmount(afterEach)

describe('pagination control names', () => {
  it.each([
    ['en', 'Pagination'],
    ['zh', '分页导航'],
    ['zh-TW', '分頁導覽'],
    ['ja', 'ページ切り替え']
  ] as const)('names navigation, page size and jump controls in %s without changing the page', (locale, navigationLabel) => {
    const messages = { en, zh, 'zh-TW': zhTW, ja }
    const i18n = createI18n({ legacy: false, locale, messages })
    const wrapper = mount(Pagination, {
      props: { total: 200, page: 2, pageSize: 20, showJump: true, showPageSizeSelector: true },
      global: { plugins: [i18n] }
    })
    expect(wrapper.get('nav').attributes('aria-label')).toBe(navigationLabel)
    expect(wrapper.get('button.select-trigger').attributes('aria-label')).toBe(messages[locale].pagination.perPage)
    expect(wrapper.get('input').attributes('aria-label')).toBe(messages[locale].pagination.jumpTo)
    expect(wrapper.get('[aria-current="page"]').text()).toBe('2')
    expect(wrapper.emitted('update:page')).toBeUndefined()
    expect(wrapper.emitted('update:pageSize')).toBeUndefined()
  })
})
