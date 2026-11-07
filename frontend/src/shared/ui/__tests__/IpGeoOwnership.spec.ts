import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { createIpGeoLookup, ipGeoLookupKey, type IpGeoLookup } from '@/shared/lib/ipGeoLookup'
import IpGeoCell from '../IpGeoCell.vue'
import IpGeoBatchToolbar from '../IpGeoBatchToolbar.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const instances: IpGeoLookup[] = []
enableAutoUnmount(afterEach)
beforeEach(() => vi.stubGlobal('localStorage', { removeItem: vi.fn() }))
afterEach(() => {
  for (const lookup of instances.splice(0)) lookup.dispose()
  vi.unstubAllGlobals()
})
function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<unknown>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function setup() {
  const scope = ref<string | null>('user7/session-a')
  const requestJson = vi.fn<(url: string, signal: AbortSignal) => Promise<unknown>>()
  const lookup = createIpGeoLookup({ scope, requestJson })
  instances.push(lookup)
  const global = { provide: { [ipGeoLookupKey as symbol]: lookup }, stubs: { Icon: true } }
  return { scope, requestJson, lookup, global }
}

describe('injected IP cell lifecycle', () => {
  it('never queries automatically and aborts its pending request when unmounted', async () => {
    const context = setup()
    const pending = deferred()
    context.requestJson.mockReturnValueOnce(pending.promise)
    const wrapper = mount(IpGeoCell, { props: { ip: '8.8.8.8' }, global: context.global })
    expect(context.requestJson).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    expect(context.lookup.getEntry('8.8.8.8').status).toBe('loading')
    wrapper.unmount()
    expect(context.requestJson.mock.calls[0][1].aborted).toBe(true)
    pending.resolve({ ip: '8.8.8.8', country_code: 'US' })
    await flushPromises()
    expect(context.lookup.getEntry('8.8.8.8').status).toBe('idle')
  })

  it('cancels the old IP when a virtualized cell is reused for another row', async () => {
    const context = setup()
    const pending = deferred()
    context.requestJson.mockReturnValueOnce(pending.promise)
    const wrapper = mount(IpGeoCell, { props: { ip: '8.8.8.8' }, global: context.global })
    await wrapper.get('button').trigger('click')
    await wrapper.setProps({ ip: '1.1.1.1' })
    expect(context.requestJson.mock.calls[0][1].aborted).toBe(true)
    expect(context.requestJson).toHaveBeenCalledTimes(1)
    pending.reject(new Error('Late failure'))
    await flushPromises()
    expect(wrapper.text()).toContain('usage.ipGeo.fetch')
    expect(context.lookup.getEntry('8.8.8.8').status).toBe('idle')
  })
})

describe('injected IP batch toolbar', () => {
  it('deduplicates only on manual click and does not query private IPs', async () => {
    const context = setup()
    context.requestJson.mockResolvedValue([{ ip: '8.8.8.8', country_code: 'US' }])
    const wrapper = mount(IpGeoBatchToolbar, { props: { ips: ['8.8.8.8', '', null, '8.8.8.8', '10.0.0.1'] }, global: context.global })
    expect(context.requestJson).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(context.requestJson.mock.calls[0][0]).toBe('https://get.geojs.io/v1/ip/geo.json?ip=8.8.8.8')
    expect(context.lookup.getEntry('10.0.0.1').status).toBe('private')
    expect(wrapper.emitted('failed')).toBeUndefined()
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })

  it.each(['network', 'malformed'])('emits failed for a current %s failure and allows retry', async failure => {
    const context = setup()
    if (failure === 'network') context.requestJson.mockRejectedValueOnce(new Error('Offline'))
    else context.requestJson.mockResolvedValueOnce({ invalid: true })
    const wrapper = mount(IpGeoBatchToolbar, { props: { ips: ['8.8.8.8'] }, global: context.global })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('failed')).toHaveLength(1)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    context.requestJson.mockResolvedValueOnce([{ ip: '8.8.8.8', country_code: 'US' }])
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(context.lookup.getEntry('8.8.8.8').status).toBe('success')
    expect(wrapper.emitted('failed')).toHaveLength(1)
  })

  it.each(['scope', 'rows', 'unmount'])('stops the batch without late failure feedback after %s changes', async cause => {
    const context = setup()
    const pending = deferred()
    context.requestJson.mockReturnValueOnce(pending.promise)
    const ips = Array.from({ length: 61 }, (_, i) => '203.0.113.' + (i + 1))
    const wrapper = mount(IpGeoBatchToolbar, { props: { ips }, global: context.global })
    await wrapper.get('button').trigger('click')
    if (cause === 'scope') context.scope.value = 'another-account'
    else if (cause === 'rows') await wrapper.setProps({ ips: ['1.1.1.1'] })
    else wrapper.unmount()
    expect(context.requestJson.mock.calls[0][1].aborted).toBe(true)
    pending.reject(new Error('Late failure'))
    await flushPromises()
    expect(context.requestJson).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('failed')).toBeUndefined()
    expect(context.lookup.getEntry(ips[0]).status).toBe('idle')
  })
})
