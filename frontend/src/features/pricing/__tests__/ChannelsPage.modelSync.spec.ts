import { listPlatformIds, supportsPricingModelSync, platformLabel } from '@/features/catalog'
import { pricingScopeKey, pricingNotificationsKey, pricingPermissionsKey, pricingCatalogKey, pricingDirectoryKey, pricingWebSearchKey } from '../ports'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/features/catalog'
import ChannelsView from '../pages/ChannelsPage.vue'

const { listChannels, createChannel, getSearchConfig, getGroups, syncPricingModels, showSuccess, showError } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  createChannel: vi.fn(),
  getSearchConfig: vi.fn(),
  getGroups: vi.fn(),
  syncPricingModels: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('../api/channels', () => ({ default: { list: listChannels, create: createChannel, syncPricingModels } }))
const scope = ref('admin:1')
const bindings = () => ({
 [pricingScopeKey as symbol]: scope,
 [pricingNotificationsKey as symbol]: { showError, showSuccess },
 [pricingPermissionsKey as symbol]: { can: () => true, canEditAdminField: () => true, isSuperAdmin: true, editableAdminFields: (_schema: string, payload: object) => payload },
 [pricingCatalogKey as symbol]: { listPlatformIds, supportsPricingModelSync, platformLabel },
 [pricingDirectoryKey as symbol]: { listGroups: getGroups, searchAccounts: vi.fn().mockResolvedValue([]), getAccount: vi.fn() },
 [pricingWebSearchKey as symbol]: async () => { const cfg = await getSearchConfig(); return cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0 }
})

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function mountView() {
  return mount(ChannelsView, {
    global: {
      provide: bindings(),
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        }),
        PricingEntryCard: defineComponent({
          props: ['entry'],
          template: '<div class="pricing-entry">{{ entry.models.join(", ") }}</div>'
        }),
        DataTable: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        Toggle: true
      }
    }
  })
}

async function openDialog(wrapper: ReturnType<typeof mountView>) {
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.channels.createChannel')!.trigger('click')
  await flushPromises()
}

async function enablePlatform(wrapper: ReturnType<typeof mountView>, platform: string) {
  const label = wrapper.findAll('label').find(label => label.text() === `admin.groups.platforms.${platform}`)
  expect(label).toBeDefined()
  await label!.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button.channel-tab').find(button => button.text() === `admin.groups.platforms.${platform}`)!.trigger('click')
}

function visibleSyncButtons(wrapper: ReturnType<typeof mountView>) {
  return wrapper.findAll('button').filter(button => button.isVisible() && [
    'admin.channels.form.syncLatestModels',
    'admin.channels.form.syncingModels'
  ].includes(button.text()))
}

describe('ChannelsView pricing model sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    createChannel.mockResolvedValue({})
    getSearchConfig.mockResolvedValue({ enabled: false })
    getGroups.mockResolvedValue([])
    syncPricingModels.mockReset()
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it('saves the OpenAI channel web search setting when global emulation is enabled', async () => {
    getSearchConfig.mockResolvedValue({ enabled: true, providers: [{ type: 'tavily' }] })
    getGroups.mockResolvedValue([{ id: 1, name: 'OpenAI group', platform: 'openai', rate_multiplier: 1 }])
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    const toggle = wrapper.findAllComponents({ name: 'Toggle' }).find(component =>
      component.element.parentElement?.textContent?.includes('admin.channels.form.webSearchEmulation')
    )
    expect(toggle).toBeDefined()
    toggle!.vm.$emit('update:modelValue', true)
    await wrapper.findAll('label').find(label => label.text().includes('OpenAI group'))!.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('input[placeholder="admin.channels.form.namePlaceholder"]').setValue('search')
    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()
    expect(createChannel).toHaveBeenCalledWith(expect.objectContaining({
      features_config: expect.objectContaining({ web_search_emulation: expect.objectContaining({ openai: true }) })
    }))
    wrapper.unmount()
  })

  it.each([
    ['openai', true],
    ['opencode_go', true],
    ['typesafe', true],
    ['command_code', false],
    ['cline', false]
  ] as const)('offers pricing model sync for %s only when its pricing catalog exists', async (platform, expected) => {
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform)

    expect(visibleSyncButtons(wrapper)).toHaveLength(expected ? 1 : 0)
    expect(syncPricingModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reacts to a new platform gaining a pricing catalog', async () => {
    const platform = { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, platform]
    })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform.id)
    expect(visibleSyncButtons(wrapper)).toHaveLength(0)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, { ...platform, litellm_provider: 'acme' }]
    })
    await flushPromises()
    expect(visibleSyncButtons(wrapper)).toHaveLength(1)
    wrapper.unmount()
  })

  it('blocks additional syncs while one request is in flight and adds its models once', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    syncPricingModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await enablePlatform(wrapper, 'opencode_go')

    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    await visibleSyncButtons(wrapper)[0].trigger('click')
    expect(syncPricingModels).toHaveBeenCalledOnce()
    expect(syncPricingModels).toHaveBeenCalledWith('openai', expect.any(AbortSignal))

    resolveSync({ models: ['gpt-latest'] })
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(1)
    expect(wrapper.get('.pricing-entry').text()).toBe('gpt-latest')
    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeUndefined()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

vi.mock('@/shared/styles/platformColors', () => ({ platformTextClass: () => '', platformBadgeLightClass: () => '', platformLabel: (platform: string) => platform, platformBorderStrongClass: () => '', platformAccentColor: () => '', platformBadgeClass: () => '', platformBorderClass: () => '', platformButtonClass: () => '' }))

vi.mock('../components/PricingEntryCard.vue', () => ({ default: { name: 'PricingEntryCard', props: ['entry'], template: '<div class="pricing-entry">{{ entry.models.join(", ") }}</div>' } }))
