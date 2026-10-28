import { afterEach, describe, it, expect, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { readonly, ref } from 'vue'
import { createIpGeoLookup, ipGeoLookupKey, type IpGeoLookup } from '@/shared/lib/ipGeoLookup'
import OpsErrorLogTable from '../OpsErrorLogTable.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledZhMessages = await loadApplicationLocale('zh')
const assembledEnMessages = await loadApplicationLocale('en')
const zhLocale = assembledZhMessages
const enLocale = assembledEnMessages

import type { OpsErrorLog } from '@/features/operations/api/ops'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const TooltipStub = { template: '<div><slot /></div>' }
const PaginationStub = { template: '<div class="pagination-stub" />' }
const geoOwners: IpGeoLookup[] = []
const geoRequests: Array<ReturnType<typeof vi.fn>> = []
enableAutoUnmount(afterEach)
afterEach(() => {
  try { for (const request of geoRequests) expect(request).not.toHaveBeenCalled() }
  finally { for (const owner of geoOwners) owner.dispose(); geoOwners.length = 0; geoRequests.length = 0 }
})

function mountTable(row: Partial<OpsErrorLog>) {
  const requestJson = vi.fn().mockRejectedValue(new Error('Unexpected geolocation request in column rendering test'))
  const geo = createIpGeoLookup({ scope: readonly(ref('ops-columns:admin')), requestJson })
  geoOwners.push(geo); geoRequests.push(requestJson)
  const base = {
    id: 1,
    created_at: '2026-06-05T23:59:50Z',
    phase: 'upstream',
    type: '',
    error_owner: 'provider',
    error_source: 'upstream_http',
    severity: 'error',
    status_code: 529,
    platform: 'anthropic',
    model: 'claude-opus-4-8',
    resolved: false,
    client_request_id: '',
    request_id: 'req-1',
    message: 'boom',
    user_email: '',
    account_name: '',
    group_name: '',
    ...row,
  } as OpsErrorLog

  return mount(OpsErrorLogTable, {
    props: { rows: [base], total: 1, loading: false, page: 1, pageSize: 20 },
    global: { provide: { [ipGeoLookupKey as symbol]: geo }, stubs: { 'el-tooltip': TooltipStub, Pagination: PaginationStub } },
  })
}

describe('OpsErrorLogTable user/api-key/account columns', () => {
  // 回归:上游错误行(phase=upstream, owner=provider)以前在单一「用户」列里只显示账号、
  // 丢失用户;现在用户/API Key/账号各占独立列,三者同时可见。
  it('renders user, api key and account in separate columns for an upstream row', () => {
    const wrapper = mountTable({
      user_id: 2,
      user_email: 'alice@test.com',
      api_key_id: 5,
      api_key_name: 'my-key',
      account_id: 9,
      account_name: 'acct-A',
    })

    const text = wrapper.text()
    expect(text).toContain('alice@test.com') // 用户列(上游行也显示用户)
    expect(text).toContain('my-key') // API Key 列
    expect(text).toContain('acct-A') // 账号列
  })

  it('shows the deleted badge for a soft-deleted api key', () => {
    const wrapper = mountTable({
      api_key_id: 5,
      api_key_name: 'old-key',
      api_key_deleted: true,
    })

    expect(wrapper.text()).toContain('old-key')
    expect(wrapper.text()).toContain('admin.ops.errorLog.keyDeletedBadge')
  })
})

describe('OpsErrorLogTable column order', () => {
  it('puts time and response content first for ops without changing time sorting', async () => {
    const wrapper = mountTable({})
    await wrapper.setProps({ summaryFirst: true })

    const headers = wrapper.findAll('thead th')
    expect(headers.slice(0, 3).map((header) => header.text())).toEqual([
      'admin.ops.errorLog.time',
      'admin.ops.errorLog.message',
      'admin.ops.errorLog.user',
    ])
    expect(wrapper.findAll('tbody td')[1].text()).toBe('boom')

    await headers[0].trigger('click')
    expect(wrapper.emitted('sort')).toEqual([['created_at', 'asc']])
    wrapper.unmount()
  })

  it('preserves the usage column order and visibility by default', async () => {
    const wrapper = mountTable({})
    await wrapper.setProps({ visibleColumnKeys: ['created_at', 'user', 'message'] })

    expect(wrapper.findAll('thead th').map((header) => header.text())).toEqual([
      'admin.ops.errorLog.user',
      'admin.ops.errorLog.message',
      'admin.ops.errorLog.time',
    ])
    wrapper.unmount()
  })
})

// 防回归:组件用 admin.ops.errorLog.* 命名空间。若 i18n 键写错命名空间(如误放到
// errorDetail),真实 vue-i18n 会回退返回 key 本身 → 界面显示原始路径字符串。
// 这里用真实 locale 校验键确实可解析(返回译文而非 key)。
// 防回归:组件用 admin.ops.errorLog.* 命名空间。若键写错命名空间(如误放到
// errorDetail),界面会显示原始路径字符串而非译文。vitest 的 vue-i18n 为 runtime-only
// (无消息编译器,t() 对任何键都回退返回 key),故直接校验 locale 对象的命名空间含这些键。
describe('OpsErrorLogTable i18n keys exist in the errorLog namespace', () => {
  const locales: Record<string, any> = { zh: zhLocale, en: enLocale }
  for (const [name, msgs] of Object.entries(locales)) {
    it(`has apiKey & keyDeletedBadge for ${name}`, () => {
      const errorLog = msgs?.admin?.ops?.errorLog
      expect(errorLog?.apiKey).toBeTruthy()
      expect(errorLog?.keyDeletedBadge).toBeTruthy()
    })
  }
})

describe('OpsErrorLogTable recovered attempts', () => {
  it('retains the provider status while making the recovered request explicit', () => {
    const wrapper = mountTable({ status_code: 503, request_status_code: 200, message: 'Recovered upstream error 503: overloaded' })
    expect(wrapper.text()).toContain('503')
    expect(wrapper.text()).toContain('admin.ops.errorLog.recovered · 200')
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({

  })
  return { ...actual, useOperationsServices: () => services }
})
