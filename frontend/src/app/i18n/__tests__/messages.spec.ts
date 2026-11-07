import { describe, expect, it, vi } from 'vitest'
import { createApplicationLocaleLoader, loadApplicationLocale } from '..'
import { mergeLocaleContributions } from '@/i18n/messageAssembly'
import type { LocaleContributionLoaders, LocaleModuleLoader } from '@/i18n/messageAssembly'

function forEveryLocale(load: LocaleModuleLoader): LocaleContributionLoaders {
  return { en: load, zh: load, 'zh-TW': load, ja: load }
}

describe('domain locale composition', () => {
  it.each([
    ['en', 'Breadcrumb'], ['zh', '导航路径'], ['zh-TW', '導覽路徑'], ['ja', 'パンくずリスト'],
  ] as const)('loads the breadcrumb landmark label in %s', async (locale, label) => {
    expect((await loadApplicationLocale(locale)).ui).toMatchObject({ breadcrumb: label })
  })
  it.each([
    ['en', 'Enter a safe, valid URL.'],
    ['zh', '请输入安全有效的链接。'],
    ['zh-TW', '請輸入安全有效的連結。'],
    ['ja', '安全で有効なURLを入力してください。']
  ] as const)('publishes the existing site validation message in %s', async (locale, message) => {
    const messages = await loadApplicationLocale(locale)
    expect(messages.siteSettings).toMatchObject({ invalidUrl: message })
  })

  it('merges shared namespaces without changing message values or inputs', () => {
    const identity = { admin: { users: { title: 'Users @:common.siteName' } } }
    const resources = { admin: { accounts: { title: 'Accounts {count}' } } }
    const before = JSON.stringify([identity, resources])
    expect(mergeLocaleContributions([
      { owner: 'identity', messages: identity },
      { owner: 'resources', messages: resources }
    ])).toEqual({ admin: { users: identity.admin.users, accounts: resources.admin.accounts } })
    expect(JSON.stringify([identity, resources])).toBe(before)
  })

  it('rejects duplicate ownership even when both values are identical', () => {
    expect(() => mergeLocaleContributions([
      { owner: 'first', messages: { admin: { title: 'Same' } } },
      { owner: 'second', messages: { admin: { title: 'Same' } } }
    ])).toThrow('Duplicate locale message admin.title: first / second')
  })

  it('rejects a message replacing a namespace in either order', () => {
    const entries = [
      { owner: 'leaf', messages: { title: 'Title' } },
      { owner: 'branch', messages: { title: { detail: 'Detail' } } }
    ]
    expect(() => mergeLocaleContributions(entries)).toThrow('Locale namespace conflicts')
    expect(() => mergeLocaleContributions([...entries].reverse())).toThrow('Duplicate locale message')
  })

  it('loads only the requested language and keeps contributions lazy', async () => {
    const en = vi.fn().mockResolvedValue({ default: { common: { loading: 'Loading' } } })
    const ja = vi.fn().mockResolvedValue({ default: { common: { loading: 'Loading-ja' } } })
    const load = createApplicationLocaleLoader({
      common: { en, zh: en, 'zh-TW': en, ja }
    })
    expect(en).not.toHaveBeenCalled()
    expect(ja).not.toHaveBeenCalled()
    expect(await load('ja')).toEqual({ common: { loading: 'Loading-ja' } })
    expect(en).not.toHaveBeenCalled()
    expect(ja).toHaveBeenCalledOnce()
  })

  it('rejects an incomplete language and retries the failed contribution', async () => {
    const common = vi.fn().mockResolvedValue({ default: { common: { loading: 'Loading' } } })
    const domain = vi.fn().mockRejectedValueOnce(new Error('chunk unavailable'))
      .mockResolvedValue({ default: { auth: { title: 'Login' } } })
    const load = createApplicationLocaleLoader({
      common: forEveryLocale(common),
      identity: forEveryLocale(domain)
    })
    await expect(load('en')).rejects.toThrow('chunk unavailable')
    expect(await load('en')).toEqual({ common: { loading: 'Loading' }, auth: { title: 'Login' } })
    expect(domain).toHaveBeenCalledTimes(2)
    await expect(createApplicationLocaleLoader({})('ja')).rejects.toThrow('Locale contributions are missing: ja')
  })
})
