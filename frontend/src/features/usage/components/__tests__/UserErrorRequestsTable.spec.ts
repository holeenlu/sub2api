import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'
import UserErrorDetailModal from '../UserErrorDetailModal.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import { usageServicesKey } from '../../services'
import { createUsageTestServices } from '../../__tests__/services'
import type { UserErrorRequest, UserErrorRequestDetail } from '../../api/types'
const getMyErrorDetail = vi.hoisted(() => vi.fn())
vi.mock('../../api/usage', () => ({ getMyErrorDetail }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => getMyErrorDetail.mockReset())
function open(scope = ref<string | null>('session-a')) {
  return mount(UserErrorRequestsTable, { props: {
    rows: [{ id: 7, client_ip: '', created_at: '2026-10-25T01:00:00Z' } as UserErrorRequest], total: 1, loading: false, page: 1, pageSize: 20,
    visibleColumnKeys: ['model', 'status'],
  }, global: { provide: { [usageServicesKey as symbol]: createUsageTestServices({ scopeKey: scope }) }, stubs: {
    DataTable: true, Pagination: true, IpGeoBatchToolbar: true, IpGeoCell: true,
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
  } } })
}
describe('user error table domain wiring', () => {
  it('keeps selected columns, sort names and pagination events', () => {
    const wrapper = open(), table = wrapper.getComponent(DataTable)
    expect(table.props('columns').map((column: { key: string }) => column.key)).toEqual(['model', 'status'])
    table.vm.$emit('sort', 'status', 'asc')
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    expect(wrapper.emitted('sort')).toEqual([['status_code', 'asc']])
    expect(wrapper.emitted('update:page')).toEqual([[2]])
    expect(wrapper.emitted('update:pageSize')).toEqual([[50]])
  })
  it('opens the actual domain modal and drops a late detail after changing session', async () => {
    const scope = ref<string | null>('session-a')
    let complete!: (value: UserErrorRequestDetail) => void
    getMyErrorDetail.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = open(scope)
    wrapper.getComponent(DataTable).vm.$emit('rowClick', { id: 7 })
    await flushPromises()
    expect(getMyErrorDetail).toHaveBeenCalledWith(7, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.getComponent(UserErrorDetailModal).props('show')).toBe(true)
    const signal = getMyErrorDetail.mock.calls[0][1].signal
    scope.value = 'session-b'
    complete({ id: 7, error_body: 'previous-session-private-body' } as UserErrorRequestDetail)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.getComponent(UserErrorDetailModal).props('show')).toBe(false)
    expect(wrapper.text()).not.toContain('previous-session-private-body')
  })
})
