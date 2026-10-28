import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '../../locales/en'
import ja from '../../locales/ja'
import zh from '../../locales/zh'
import zhTW from '../../locales/zh-TW'
const messages = { en, ja, zh, 'zh-TW': zhTW }

describe('account-level BPS configuration copy', () => {
  it.each([
    { locale: 'zh', noFallback: '不会触发原生回退', noMigration: '不会将 previous_response_id', reauthorization: '需重新授权，不安排生成探测' },
    { locale: 'zh-TW', noFallback: '不會觸發原生回退', noMigration: '不會將 previous_response_id', reauthorization: '需重新授權，不安排生成探測' },
    { locale: 'en', noFallback: 'do not fall back to native', noMigration: 'does not move previous_response_id', reauthorization: 'require reauthorization, without generation probes' },
    { locale: 'ja', noFallback: 'ネイティブへフォールバックしません', noMigration: 'キャンセルをネイティブへ移しません', reauthorization: '再認証が必要で、生成探測を行いません' }
  ])('retains explicit selection and recovery boundaries in $locale', ({ locale, noFallback, noMigration, reauthorization }) => {
    const i18n = createI18n({ legacy: false, locale, messages })
    const t = (key: string) => i18n.global.t('admin.accounts.openai.' + key)
    expect(t('excelBPSModelsHint')).toContain(noFallback)
    expect(t('excelBPSModelsHint')).toContain('Cloud')
    expect(t('excelBPSModelsHint')).toContain('Excel-only')
    expect(t('excelBPSNotice')).toContain(noMigration)
    expect(t('excelBPSAutoRecoverOn403Desc')).toContain(reauthorization)
    expect(t('excelBPSAccountSettingsHint')).not.toBe('admin.accounts.openai.excelBPSAccountSettingsHint')
  })
})

// These strings describe server facts only; scope parsing and routing have separate owners.
describe('BPS scope locale contracts', () => {
  it.each(Object.keys(messages))('interpolates published scope labels in %s', locale => {
    const i18n = createI18n({ legacy: false, locale, messages })
    const t = (key: string) => i18n.global.t('admin.accounts.status.' + key, { model: 'selected-model', endpoint: '/responses', time: '12:00' })
    for (const key of ['bpsScopeAccount', 'bpsScopeUnknown']) expect(t(key)).toContain('BPS')
    for (const key of ['bpsScopeModel', 'bpsScopeModelEndpoint', 'bpsModelPermission']) expect(t(key)).toContain('selected-model')
    for (const key of ['bpsScopeEndpoint', 'bpsScopeModelEndpoint']) expect(t(key)).toContain('/responses')
    expect(t('bpsCooldownUntil')).toContain('12:00')
    expect(t('bpsCooldownUntil')).not.toContain('{time}')
  })
})
