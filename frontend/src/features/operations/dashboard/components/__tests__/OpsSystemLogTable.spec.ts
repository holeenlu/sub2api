import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import OpsSystemLogTable from '../OpsSystemLogTable.vue'
import LogRetentionSelect from '../LogRetentionSelect.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const enLocale = assembledEnMessages
const zhLocale = assembledZhMessages


const mockListSystemLogs = vi.fn()
const mockCleanupSystemLogs = vi.fn()
const mockGetSystemLogSinkHealth = vi.fn()
const mockGetRuntimeLogConfig = vi.fn()
const mockUpdateRuntimeLogConfig = vi.fn()
const mockShowError = vi.fn()

vi.mock('@/features/operations/api/ops', () => ({
  opsAPI: {
    listSystemLogs: (...args: any[]) => mockListSystemLogs(...args),
    cleanupSystemLogs: (...args: any[]) => mockCleanupSystemLogs(...args),
    getSystemLogSinkHealth: (...args: any[]) => mockGetSystemLogSinkHealth(...args),
    getRuntimeLogConfig: (...args: any[]) => mockGetRuntimeLogConfig(...args),
    updateRuntimeLogConfig: (...args: any[]) => mockUpdateRuntimeLogConfig(...args),
  },
}))


vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const SelectStub = defineComponent({
  name: 'SelectControlStub',
  props: {
    modelValue: {
      type: [String, Number],
      default: '',
    },
  },
  emits: ['update:modelValue'],
  template: '<div class="select-stub" />',
})

const PaginationStub = defineComponent({
  name: 'PaginationStub',
  template: '<div class="pagination-stub" />',
})

const runtimeConfig = {
  level: 'info',
  persist_access_logs: false,
  enable_sampling: false,
  sampling_initial: 100,
  sampling_thereafter: 100,
  caller: true,
  stacktrace_level: 'error',
  retention_days: 30,
  request_retention_days: 90,
}

const sinkHealth = {
  queue_depth: 0,
  queue_capacity: 5000,
  dropped_count: 0,
  write_failed_count: 0,
  written_count: 1,
  avg_write_delay_ms: 0,
}

describe('OpsSystemLogTable host support', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    mockListSystemLogs.mockResolvedValue({
      items: [
        {
          id: 1,
          created_at: '2026-07-14T00:10:01Z',
          host: 'api-node-1',
          level: 'warn',
          component: 'app',
          message: 'request failed',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
    })
    mockCleanupSystemLogs.mockResolvedValue({ deleted: 1 })
    mockGetSystemLogSinkHealth.mockResolvedValue(sinkHealth)
    mockGetRuntimeLogConfig.mockResolvedValue(runtimeConfig)
    mockUpdateRuntimeLogConfig.mockImplementation(async config => config)
  })

  it('renders the host and sends it with list and cleanup filters', async () => {
    const wrapper = mount(OpsSystemLogTable, {
      global: {
        stubs: {
          Select: SelectStub,
          Pagination: PaginationStub,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('api-node-1')

    const hostLabel = wrapper.findAll('label').find((label) => label.text().includes('admin.ops.systemLogs.host'))
    expect(hostLabel).toBeDefined()
    await hostLabel!.find('input').setValue(' api-node-2 ')

    const searchButton = wrapper.findAll('button').find((button) => button.text() === 'admin.ops.systemLogs.search')
    expect(searchButton).toBeDefined()
    await searchButton!.trigger('click')
    await flushPromises()

    expect(mockListSystemLogs).toHaveBeenLastCalledWith(expect.objectContaining({ host: 'api-node-2' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))

    const cleanupButton = wrapper.findAll('button').find((button) => button.text() === 'admin.ops.systemLogs.cleanCurrentFilters')
    expect(cleanupButton).toBeDefined()
    await cleanupButton!.trigger('click')
    await flushPromises()

    expect(mockCleanupSystemLogs).toHaveBeenCalledWith(expect.objectContaining({ host: 'api-node-2' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('renders scalar extension fields without turning zero/false into missing data or objects into text', async () => {
    mockListSystemLogs.mockResolvedValue({
      items: [{
        id: 2, created_at: '2026-10-26T00:00:00Z', host: 'api-node-1', level: 'warn', component: 'app',
        message: 'original failure',
        extra: { latency_ms: 0, status_code: 0, errors: false, err: { secret: 'not printable' }, error: 'source rejection', path: null }
      }], total: 1
    })
    const wrapper = mount(OpsSystemLogTable, { global: { stubs: { Select: SelectStub, Pagination: PaginationStub } } })
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('original failure')
      expect(wrapper.text()).toContain('latency_ms=0')
      expect(wrapper.text()).toContain('status=0')
      expect(wrapper.text()).toContain('errors=false')
      expect(wrapper.text()).toContain('error=source rejection')
      expect(wrapper.text()).not.toContain('[object Object]')
      expect(wrapper.text()).not.toContain('not printable')
      expect(wrapper.text()).not.toContain('path=null')
    } finally { wrapper.unmount() }
  })

  it('keeps database access-log persistence opt-in', async () => {
    const wrapper = mount(OpsSystemLogTable, {
      global: {
        stubs: {
          Select: SelectStub,
          Pagination: PaginationStub,
        },
      },
    })
    await flushPromises()

    const label = wrapper.findAll('label').find((item) => item.text().includes('admin.ops.systemLogs.persistAccessLogs'))
    expect(label).toBeDefined()
    expect((label!.find('input').element as HTMLInputElement).checked).toBe(false)
  })

  it.each([
    ['zh', zhLocale],
    ['en', enLocale],
  ])('defines the Host translation for %s', (_name, locale) => {
    expect(locale.admin.ops.systemLogs.host).toBe('Host')
  })
})

describe('rolling log retention settings', () => {
  const mountTable = () => mount(OpsSystemLogTable, {
    global: { stubs: { Select: SelectStub, Pagination: PaginationStub } }
  })
  const saveButton = (wrapper: ReturnType<typeof mountTable>) => wrapper.findAll('button').find(button => button.text() === 'admin.ops.systemLogs.saveAndApply')!

  beforeEach(() => {
    vi.clearAllMocks()
    mockListSystemLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    mockGetSystemLogSinkHealth.mockResolvedValue(sinkHealth)
    mockGetRuntimeLogConfig.mockResolvedValue(runtimeConfig)
    mockUpdateRuntimeLogConfig.mockImplementation(async config => config)
  })

  it('loads and saves separate operations and request retention windows', async () => {
    const wrapper = mountTable()
    await flushPromises()
    const fields = wrapper.findAllComponents(LogRetentionSelect)
    expect(fields.map(field => field.props('modelValue'))).toEqual([30, 90])
    fields[0]!.vm.$emit('update:modelValue', 7)
    fields[1]!.vm.$emit('update:modelValue', 180)
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(mockUpdateRuntimeLogConfig).toHaveBeenCalledWith(expect.objectContaining({ retention_days: 7, request_retention_days: 180 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    wrapper.unmount()
  })

  it('supports forever and rejects fractional or empty custom days', async () => {
    const wrapper = mountTable()
    await flushPromises()
    const requestField = wrapper.findAllComponents(LogRetentionSelect)[1]!
    requestField.vm.$emit('update:modelValue', 0)
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(mockUpdateRuntimeLogConfig).toHaveBeenCalledWith(expect.objectContaining({ request_retention_days: 0 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    mockUpdateRuntimeLogConfig.mockClear()
    for (const value of [1.5, Number.NaN, -1, 3651]) {
      requestField.vm.$emit('update:modelValue', value)
      await saveButton(wrapper).trigger('click')
    }
    expect(mockUpdateRuntimeLogConfig).not.toHaveBeenCalled()
    expect(mockShowError).toHaveBeenCalledWith('admin.ops.systemLogs.retentionDaysInvalid')
    wrapper.unmount()
  })

  it('does not overwrite saved retention with defaults after a load failure', async () => {
    mockGetRuntimeLogConfig.mockRejectedValue(new Error('unavailable'))
    const wrapper = mountTable()
    await flushPromises()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    await saveButton(wrapper).trigger('click')
    expect(mockUpdateRuntimeLogConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({
    notifyError: (...args: any[]) => mockShowError(...args),
    notifySuccess: vi.fn(),
  })
  return { ...actual, useOperationsServices: () => services }
})
