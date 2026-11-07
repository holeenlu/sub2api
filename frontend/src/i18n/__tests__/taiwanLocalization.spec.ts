import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { dirname, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { execFileSync } from 'node:child_process'
import { loadApplicationLocale } from '@/app/i18n'
const assembledZhMessages = await loadApplicationLocale('zh')
const assembledZhTWMessages = await loadApplicationLocale('zh-TW')
const assembledEnMessages = await loadApplicationLocale('en')
const assembledJaMessages = await loadApplicationLocale('ja')
const zh = assembledZhMessages
const zhTW = assembledZhTWMessages
const en = assembledEnMessages
const ja = assembledJaMessages




const tools = resolve(dirname(fileURLToPath(import.meta.url)), '../../../../tools/zh-tw')

describe('site-wide Taiwan localization', () => {
  it('converts historical commission sources without changing identifiers or losing idempotence', () => {
    const result = JSON.parse(execFileSync(process.execPath, [
      '--input-type=module',
      '-e',
      'const { convertSource, hasSimplified } = await import(process.argv[1]); ' +
        'const text = convertSource(process.argv[2]); ' +
        'process.stdout.write(JSON.stringify({ text, repeated: convertSource(text), simplified: hasSimplified(text) }));',
      pathToFileURL(resolve(tools, 'convert.mjs')).href,
      "affiliate: '返佣转入'",
    ], { encoding: 'utf8' }))
    expect(result).toEqual({
      text: "affiliate: '返傭轉入'",
      repeated: "affiliate: '返傭轉入'",
      simplified: false,
    })
  })

  it('keeps Chinese UI literals in the locale catalogs', () => {
    const output = execFileSync(process.execPath, [resolve(tools, 'audit-ui.mjs')], { encoding: 'utf8' })
    expect(output).toContain('0 untranslated Chinese literals')
  })

  it('checks generated messages for simplified characters', () => {
    const output = execFileSync(process.execPath, [resolve(tools, 'audit-locale.mjs')], { encoding: 'utf8' })
    expect(output).toContain('簡體殘留行數：0')
  })

  it('uses contextual Taiwan terms in the dashboard and navigation', () => {
    expect(zhTW.dashboard.platformBreakdown).toBe('依平台分類')
    expect(zhTW.nav.collapse).toBe('摺疊')
    expect(zhTW.nav.darkMode).toBe('深色模式')
    expect(zhTW.ui.chartCacheRead).toBe('快取讀取')
    expect(zhTW.ui.chartCacheHitRate).toBe('快取命中率')
    expect(zhTW.ui.mobileApp).toBe('行動應用程式')
  })

  it.each(['zh', 'zh-TW', 'ja', 'en'] as const)('renders batch instructions without corrupting protocol examples in %s', locale => {
    const i18n = createI18n({ legacy: false, locale, messages: { zh, 'zh-TW': zhTW, ja, en }, warnHtmlMessage: false })
    const request = '{"model":"gemini-3-pro-image","output_count":4}'
    const output = i18n.global.t('ui.batchAgentInstruction', {
      endpoint: 'https://example.invalid', skillName: 'test-batch-image', requestExample: request,
      modelsUrl: '/v1/images/batches/models', submitUrl: '/v1/images/batches',
      statusUrl: '/v1/images/batches/{id}', itemsUrl: '/v1/images/batches/{id}/items',
      downloadUrl: '/v1/images/batches/{id}/download', cancelUrl: '/v1/images/batches/{id}/cancel',
    })
    expect(output).toContain(request)
    expect(output).toContain('GET /v1/images/batches/{id}')
    expect(output).toContain('200')
    expect(output).toContain('1000')
    expect(output).toContain('128MB')
    expect(output).not.toContain('{endpoint}')
    if (locale === 'en') expect(output).not.toMatch(/[\u3400-\u9fff]/)
  })
})
