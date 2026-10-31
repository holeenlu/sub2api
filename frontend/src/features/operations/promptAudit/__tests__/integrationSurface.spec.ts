import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const en = assembledEnMessages
const zh = assembledZhMessages

import { operationsRoutes } from '../../routes'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')

describe('Prompt Audit integration surface', () => {
  it('contributes the existing admin and risk-control guarded route to the app shell', () => {
    const route = operationsRoutes.find(item => item.name === 'AdminPromptAudit')
    expect(route?.path).toBe('/admin/prompt-audit')
    expect(route?.meta).toMatchObject({ shell: 'admin', requiresAuth: true, requiresAdmin: true, requiresRiskControl: true })
  })

  it('keeps layout ownership in app and only imports this domain at runtime', () => {
    const page = read('../PromptAuditView.vue')
    expect(page).not.toContain('@/app/')
    expect(page).not.toContain('@/stores')
    expect(page).not.toContain('@/features/identity')
    expect(page).toContain('useOperationsServices')
  })

  it('keeps Prompt Audit locale trees symmetric and all operational controls named', () => {
    expect(Object.keys(zh.admin.promptAudit)).toEqual(Object.keys(en.admin.promptAudit))
    expect(zh.nav.securityAudit).toBeTruthy()
    expect(en.nav.securityAudit).toBeTruthy()
    const endpoint = read('../components/EndpointPool.vue')
    const events = read('../components/EventWorkspace.vue')
    expect(endpoint).toContain('aria-label')
    expect(events).toContain('aria-label')
    expect(events).toContain('overflow-x-auto')
    expect(events).toContain('sm:grid-cols-2')
  })
})
