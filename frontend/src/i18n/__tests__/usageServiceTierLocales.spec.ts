import { describe, expect, it } from 'vitest'

import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const en = assembledEnMessages
const zh = assembledZhMessages


describe('usage service tier locale keys', () => {
  it('contains zh labels for service tier tooltip', () => {
    expect(zh.usage.serviceTier).toBe('服务档位')
    expect(zh.usage.serviceTierPriority).toBe('Fast')
    expect(zh.usage.serviceTierUltrafast).toBe('Ultrafast')
    expect(zh.usage.serviceTierFlex).toBe('Flex')
    expect(zh.usage.serviceTierStandard).toBe('Standard')
  })

  it('contains en labels for service tier tooltip', () => {
    expect(en.usage.serviceTier).toBe('Service tier')
    expect(en.usage.serviceTierPriority).toBe('Fast')
    expect(en.usage.serviceTierUltrafast).toBe('Ultrafast')
    expect(en.usage.serviceTierFlex).toBe('Flex')
    expect(en.usage.serviceTierStandard).toBe('Standard')
  })
})
